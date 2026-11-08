package system

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const panelUpdatePath = "/api/v1/system/panel-update"

// panelUpdateFixedNow is the clock every panel update case starts from. The contract
// files carry absolute RFC3339 timestamps and the stalled/interrupted thresholds are
// measured against the clock, so the cases pin it instead of reading the wall clock: it
// is five minutes after the 10:00 timestamps they write.
var panelUpdateFixedNow = time.Date(2026, 11, 8, 10, 5, 0, 0, time.UTC)

// panelUpdateFixture wires the shared Owner/Viewer fixture to a panel update service
// that talks to a stub release endpoint and owns one temporary helper directory.
func panelUpdateFixture(t *testing.T, current, directory string) (*installHTTPFixture, *releaseStub) {
	t.Helper()
	fixture := newInstallHTTPFixture(t)
	stub := &releaseStub{}
	stub.set(0, releaseBody150)
	fixture.system.PanelUpdate = &PanelUpdateService{Directory: directory, CurrentVersion: current,
		BaseURL: stub.server(t).URL, Client: &http.Client{}, Now: func() time.Time { return panelUpdateFixedNow }}
	return fixture, stub
}

func panelUpdateState(t *testing.T, fixture *installHTTPFixture) PanelUpdateState {
	t.Helper()
	response := fixture.at(t, "GET", panelUpdatePath, "", fixture.owner, "")
	if response.Code != 200 {
		t.Fatalf("owner GET: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET cache header: %q", response.Header().Get("Cache-Control"))
	}
	var state PanelUpdateState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatalf("GET body: %v %s", err, response.Body.String())
	}
	return state
}

func writeContractFile(t *testing.T, directory, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPanelUpdateStateHTTPOwnerOnlyAndUnsupported(t *testing.T) {
	fixture, _ := panelUpdateFixture(t, "1.4.3", t.TempDir())
	if code := fixture.at(t, "GET", panelUpdatePath, "", fixture.viewer, "").Code; code != 403 {
		t.Fatalf("viewer GET: %d", code)
	}
	state := panelUpdateState(t, fixture)
	if !state.Supported || state.UnsupportedReason != nil || !state.UpdateAvailable || state.Latest == nil || state.Latest.Version != "1.5.0" {
		t.Fatalf("supported state: %+v", state)
	}
	if state.Pending != nil || state.Status != nil || state.LatestError != nil {
		t.Fatalf("fresh state carries state: %+v", state)
	}
	// An rc build has no release archive to download, and a panel without the helper
	// directory has nothing to ask: both are unsupported but still show the versions.
	fixture.system.PanelUpdate.CurrentVersion = "1.5.0-rc.1"
	rc := panelUpdateState(t, fixture)
	if rc.Supported || rc.UnsupportedReason == nil || *rc.UnsupportedReason != "dev_build" || rc.UpdateAvailable || rc.Latest == nil {
		t.Fatalf("rc state: %+v", rc)
	}
	fixture.system.PanelUpdate.CurrentVersion = "1.4.3"
	fixture.system.PanelUpdate.Directory = filepath.Join(t.TempDir(), "absent")
	missing := panelUpdateState(t, fixture)
	if missing.Supported || missing.UnsupportedReason == nil || *missing.UnsupportedReason != "helper_missing" || missing.UpdateAvailable {
		t.Fatalf("missing helper state: %+v", missing)
	}
}

func TestPanelUpdateStateHTTPPendingAndStatus(t *testing.T) {
	directory := t.TempDir()
	fixture, _ := panelUpdateFixture(t, "1.4.3", directory)
	const picked = `{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","requestedAt":"2026-11-08T10:00:00Z","requestedBy":"owner"}`
	writeContractFile(t, directory, panelUpdateRequestFile, picked)
	writeContractFile(t, directory, panelUpdateStatusFile,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","previousVersion":"1.4.3","state":"succeeded","code":"succeeded","startedAt":"2026-11-08T10:00:01Z","finishedAt":"2026-11-08T10:01:10Z","extra":"tolerated"}`)
	// The helper has answered the request, so nothing is pending any more.
	state := panelUpdateState(t, fixture)
	if state.Pending != nil || state.Status == nil || state.Status.State != "succeeded" || state.Status.PreviousVersion != "1.4.3" {
		t.Fatalf("answered request: %+v", state)
	}
	// A newer request id means the helper has not picked it up yet.
	writeContractFile(t, directory, panelUpdateRequestFile,
		`{"requestId":"fedcba9876543210fedcba9876543210","version":"1.6.0","requestedAt":"2026-11-08T10:04:00Z","requestedBy":"owner"}`)
	state = panelUpdateState(t, fixture)
	if state.Pending == nil || state.Pending.Version != "1.6.0" || state.Pending.RequestedBy != "owner" {
		t.Fatalf("pending request: %+v", state.Pending)
	}
	// An unreadable, unknown or contract-violating status is no status at all.
	for _, content := range []string{
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","state":"weird","code":"succeeded"}`,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","state":"failed","code":"exploded"}`,
		`{"requestId":"not-hex","version":"1.5.0","state":"failed","code":"update_failed"}`,
		`not json`,
	} {
		writeContractFile(t, directory, panelUpdateStatusFile, content)
		if state := panelUpdateState(t, fixture); state.Status != nil {
			t.Fatalf("invalid status %s read as %+v", content, state.Status)
		}
	}
}

func TestPanelUpdateSubmitHTTPChecksAndWritesRequest(t *testing.T) {
	directory := t.TempDir()
	fixture, stub := panelUpdateFixture(t, "1.4.3", directory)
	post := func(body, cookie, csrf string) (int, string) {
		t.Helper()
		response := fixture.at(t, "POST", panelUpdatePath, body, cookie, csrf)
		return response.Code, response.Body.String()
	}
	if code, _ := post(`{"version":"1.5.0"}`, fixture.viewer, fixture.viewerCSRF); code != 403 {
		t.Fatalf("viewer POST: %d", code)
	}
	if code, _ := post(`{"version":"1.5.0"}`, fixture.owner, ""); code != 403 {
		t.Fatalf("owner POST without CSRF: %d", code)
	}
	for _, body := range []string{`{"version":"1.5"}`, `{"version":"v1.5.0"}`, `{"version":"1.5.0-rc.1"}`, `{"version":"1.5.0","extra":1}`} {
		if code, message := post(body, fixture.owner, fixture.ownerCSRF); code != 400 {
			t.Fatalf("invalid body %s: %d %s", body, code, message)
		}
	}
	// Nothing was written by any rejected request.
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatalf("rejected requests wrote %v %v", entries, err)
	}
	// A development build cannot update itself.
	fixture.system.PanelUpdate.CurrentVersion = ""
	if code, message := post(`{"version":"1.5.0"}`, fixture.owner, fixture.ownerCSRF); code != 409 || !strings.Contains(message, "panel_update_unsupported") {
		t.Fatalf("unsupported POST: %d %s", code, message)
	}
	fixture.system.PanelUpdate.CurrentVersion = "1.4.3"
	// An unanswered request, or a running update, blocks a second one.
	writeContractFile(t, directory, panelUpdateRequestFile,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","requestedAt":"2026-11-08T10:00:00Z","requestedBy":"owner"}`)
	if code, message := post(`{"version":"1.5.0"}`, fixture.owner, fixture.ownerCSRF); code != 409 || !strings.Contains(message, "panel_update_busy") {
		t.Fatalf("pending POST: %d %s", code, message)
	}
	os.Remove(filepath.Join(directory, panelUpdateRequestFile))
	writeContractFile(t, directory, panelUpdateStatusFile,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","state":"running","code":"running","startedAt":"2026-11-08T10:00:01Z"}`)
	if code, message := post(`{"version":"1.5.0"}`, fixture.owner, fixture.ownerCSRF); code != 409 || !strings.Contains(message, "panel_update_busy") {
		t.Fatalf("running POST: %d %s", code, message)
	}
	os.Remove(filepath.Join(directory, panelUpdateStatusFile))
	// A target that is not the newest release, or not newer than the panel, is stale.
	if code, message := post(`{"version":"1.4.9"}`, fixture.owner, fixture.ownerCSRF); code != 409 || !strings.Contains(message, "panel_update_stale") || !strings.Contains(message, "1.5.0") {
		t.Fatalf("stale POST: %d %s", code, message)
	}
	// A failed release lookup is a 502 that never repeats the upstream text. It needs
	// its own service: the successful answer above is cached for ten minutes.
	failing, failingStub := panelUpdateFixture(t, "1.4.3", t.TempDir())
	failingStub.set(503, `upstream maintenance`)
	if response := failing.at(t, "POST", panelUpdatePath, `{"version":"1.5.0"}`, failing.owner, failing.ownerCSRF); response.Code != 502 || !strings.Contains(response.Body.String(), "panel_update_check_failed") || strings.Contains(response.Body.String(), "maintenance") {
		t.Fatalf("check failed POST: %d %s", response.Code, response.Body.String())
	}
	stub.set(0, releaseBody150)
	code, message := post(`{"version":"1.5.0"}`, fixture.owner, fixture.ownerCSRF)
	if code != 202 {
		t.Fatalf("owner POST: %d %s", code, message)
	}
	var answer struct {
		RequestID string `json:"requestId"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal([]byte(message), &answer); err != nil || answer.Version != "1.5.0" || !panelUpdateRequestIDPattern.MatchString(answer.RequestID) {
		t.Fatalf("202 answer: %s %v", message, err)
	}
	// The request file is one line of the fixed contract, written for the helper.
	info, err := os.Stat(filepath.Join(directory, panelUpdateRequestFile))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("request mode: %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(filepath.Join(directory, panelUpdateRequestFile))
	if err != nil || strings.Count(string(data), "\n") > 0 {
		t.Fatalf("request file: %q %v", data, err)
	}
	if !strings.Contains(string(data), `"requestId":"`+answer.RequestID+`"`) || !strings.Contains(string(data), `"version":"1.5.0"`) ||
		!strings.Contains(string(data), `"requestedBy":"owner"`) || !strings.Contains(string(data), `"requestedAt":"`) {
		t.Fatalf("request content: %s", data)
	}
	// The submission is now pending and appears in the state the card polls.
	if state := panelUpdateState(t, fixture); state.Pending == nil || state.Pending.RequestID != answer.RequestID {
		t.Fatalf("state after POST: %+v", state.Pending)
	}
	var audits int
	if err := fixture.repository.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='更新面板' AND target='panel' AND result='成功' AND operator='owner'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit rows: %d", audits)
	}
}

// A request nothing answers and a status nothing finishes must not leave the card busy
// forever: the helper unit may not be running at all, or a reboot may have killed it
// mid-update after qui-update's boot recovery had already put the old version back.
func TestPanelUpdateStalledAndInterruptedHTTP(t *testing.T) {
	directory := t.TempDir()
	fixture, _ := panelUpdateFixture(t, "1.4.3", directory)
	now := panelUpdateFixedNow
	fixture.system.PanelUpdate.Now = func() time.Time { return now }
	post := func() int {
		t.Helper()
		return fixture.at(t, "POST", panelUpdatePath, `{"version":"1.5.0"}`, fixture.owner, fixture.ownerCSRF).Code
	}
	const stamped = "2026-11-08T10:05:00Z"
	writeContractFile(t, directory, panelUpdateRequestFile,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","requestedAt":"`+stamped+`","requestedBy":"owner"}`)
	// Just below the threshold the request is work in progress: GET reports it without
	// the stalled flag and a second request is refused.
	now = panelUpdateFixedNow.Add(panelUpdateStalledAfter - time.Second)
	if state := panelUpdateState(t, fixture); state.Pending == nil || state.Pending.Stalled {
		t.Fatalf("pending request below the threshold: %+v", state.Pending)
	}
	if code := post(); code != 409 {
		t.Fatalf("pending POST below the threshold: %d", code)
	}
	// Just above it nothing is working on the request any more, so it may be replaced.
	now = panelUpdateFixedNow.Add(panelUpdateStalledAfter + time.Second)
	if state := panelUpdateState(t, fixture); state.Pending == nil || !state.Pending.Stalled {
		t.Fatalf("stalled pending request: %+v", state.Pending)
	}
	if code := post(); code != 202 {
		t.Fatalf("stalled POST: %d", code)
	}
	if state := panelUpdateState(t, fixture); state.Pending == nil || state.Pending.Stalled ||
		state.Pending.RequestID == "0123456789abcdef0123456789abcdef" {
		t.Fatalf("request after the stalled overwrite: %+v", state.Pending)
	}
	os.Remove(filepath.Join(directory, panelUpdateRequestFile))
	// A running status is an update in progress until it is older than the helper unit's
	// own start timeout; a reboot leaves such a status behind for good.
	writeContractFile(t, directory, panelUpdateStatusFile,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","state":"running","code":"running","startedAt":"`+stamped+`"}`)
	now = panelUpdateFixedNow.Add(panelUpdateInterruptedAfter - time.Second)
	if state := panelUpdateState(t, fixture); state.Status == nil || state.Status.Interrupted {
		t.Fatalf("running status below the threshold: %+v", state.Status)
	}
	if code := post(); code != 409 {
		t.Fatalf("running POST below the threshold: %d", code)
	}
	now = panelUpdateFixedNow.Add(panelUpdateInterruptedAfter + time.Second)
	if state := panelUpdateState(t, fixture); state.Status == nil || !state.Status.Interrupted {
		t.Fatalf("interrupted running status: %+v", state.Status)
	}
	if code := post(); code != 202 {
		t.Fatalf("interrupted POST: %d", code)
	}
	// A running status without a readable start time cannot be followed either.
	writeContractFile(t, directory, panelUpdateStatusFile,
		`{"requestId":"0123456789abcdef0123456789abcdef","version":"1.5.0","state":"running","code":"running"}`)
	if state := panelUpdateState(t, fixture); state.Status == nil || !state.Status.Interrupted {
		t.Fatalf("running status without startedAt: %+v", state.Status)
	}
}

// The helper never deletes request.json, so after a finished update the file still names
// the request while status.json says the run is over. Such an answered request is not
// busy: otherwise every later submission would be refused for ten minutes after a
// successful update, and POST would disagree with GET, which reports no pending request
// once the status names it.
func TestPanelUpdateAnsweredRequestDoesNotBlockSubmit(t *testing.T) {
	directory := t.TempDir()
	fixture, stub := panelUpdateFixture(t, "1.4.3", directory)
	stub.set(0, `{"tag_name":"v1.6.0","html_url":"https://github.com/jingniao/qui/releases/tag/v1.6.0","published_at":"2026-11-05T10:00:00Z","draft":false,"prerelease":false}`)
	const answered = "0123456789abcdef0123456789abcdef"
	writeContractFile(t, directory, panelUpdateRequestFile,
		`{"requestId":"`+answered+`","version":"1.5.0","requestedAt":"2026-11-08T10:00:00Z","requestedBy":"owner"}`)
	writeContractFile(t, directory, panelUpdateStatusFile,
		`{"requestId":"`+answered+`","version":"1.5.0","previousVersion":"1.4.3","state":"succeeded","code":"succeeded","startedAt":"2026-11-08T10:00:01Z","finishedAt":"2026-11-08T10:01:10Z"}`)
	// Well inside panelUpdateStalledAfter, so the old request is not stalled either: the
	// helper has simply answered it and left the file behind.
	if state := panelUpdateState(t, fixture); state.Pending != nil || state.Status == nil || state.Status.RequestID != answered {
		t.Fatalf("answered request was reported pending: %+v %+v", state.Pending, state.Status)
	}
	response := fixture.at(t, "POST", panelUpdatePath, `{"version":"1.6.0"}`, fixture.owner, fixture.ownerCSRF)
	if response.Code != 202 {
		t.Fatalf("POST after an answered request: %d %s", response.Code, response.Body.String())
	}
	var answer struct {
		RequestID string `json:"requestId"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil || answer.Version != "1.6.0" ||
		!panelUpdateRequestIDPattern.MatchString(answer.RequestID) || answer.RequestID == answered {
		t.Fatalf("202 answer: %s %v", response.Body.String(), err)
	}
	// The file now holds the new request, which the old status does not answer, so it is
	// pending again and a second submission has to wait.
	if state := panelUpdateState(t, fixture); state.Pending == nil || state.Pending.RequestID != answer.RequestID {
		t.Fatalf("state after the answered POST: %+v", state.Pending)
	}
	if second := fixture.at(t, "POST", panelUpdatePath, `{"version":"1.6.0"}`, fixture.owner, fixture.ownerCSRF); second.Code != 409 ||
		!strings.Contains(second.Body.String(), "panel_update_busy") {
		t.Fatalf("unanswered POST: %d %s", second.Code, second.Body.String())
	}
}
