package system

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The panel runs as the unprivileged proxy-panel user and cannot replace its own
// binary. It only drops request.json into the directory the installer created for
// the root helper, which runs qui-update --component panel (database backup,
// health check, rollback) and writes status.json back. Both files are the whole
// contract with the helper, so their shapes are fixed here.
const (
	// DefaultPanelUpdateDirectory is where the helper waits for a request. The
	// installer creates it owned by proxy-panel 0700; when it does not exist this
	// panel has no update helper installed and the feature is unsupported.
	DefaultPanelUpdateDirectory = "/var/lib/proxy-panel/panel-update"

	panelUpdateRequestFile = "request.json"
	panelUpdateStatusFile  = "status.json"
	// panelUpdateFileLimit bounds both files. The helper writes a handful of short
	// fields, so 4 KiB is generous and keeps a corrupt file harmless.
	panelUpdateFileLimit = 4 << 10
	panelUpdateBodyLimit = 256 << 10
	panelUpdateTimeout   = 10 * time.Second
	panelUpdateCacheTTL  = 10 * time.Minute

	// panelUpdateStalledAfter is how long a request may wait for the helper before the
	// panel stops treating it as work in progress. The path unit that starts the helper is
	// the only thing that picks a request up, so a request nothing answers - the unit not
	// running, or a status the panel could not accept - must not block the card forever.
	panelUpdateStalledAfter = 10 * time.Minute
	// panelUpdateInterruptedAfter is when a status left in "running" cannot belong to a
	// live helper any more: proxy-panel-update.service has TimeoutStartSec=1800, and an
	// update killed by a reboot is finished by qui-update's own boot recovery instead.
	panelUpdateInterruptedAfter = 35 * time.Minute

	defaultPanelUpdateBaseURL = "https://api.github.com/repos/jingniao/qui/releases/latest"
	panelUpdateUserAgent      = "qui-panel"

	panelUpdateRunning   = "running"
	panelUpdateSucceeded = "succeeded"
	panelUpdateFailed    = "failed"
)

// officialVersionPattern is the release version a web update can target: three
// numbers and nothing else. A candidate (-rc.N) or a development build has no
// release archive to download, so it never updates itself from the web UI.
var officialVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var panelUpdateRequestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

var panelUpdateStatusCodes = map[string]bool{
	panelUpdateRunning:   true,
	panelUpdateSucceeded: true,
	"invalid_request":    true,
	"not_newer":          true,
	"update_failed":      true,
	"version_mismatch":   true,
}

var (
	// ErrPanelUpdateBusy reports a request the helper has not answered yet or an
	// update in progress: a second request would race the running update.
	ErrPanelUpdateBusy = errors.New("panel update already pending or running")
	// ErrPanelUpdateCheckFailed reports that the latest official release could not
	// be read. Upstream text is never echoed to the client.
	ErrPanelUpdateCheckFailed = errors.New("latest panel release lookup failed")
)

// PanelUpdateUnsupportedError carries why the running panel cannot update itself
// from the web UI: a development/candidate build, or no helper directory.
type PanelUpdateUnsupportedError struct{ Reason string }

func (e PanelUpdateUnsupportedError) Error() string { return "panel update unsupported: " + e.Reason }

// PanelUpdateStaleError reports a target that is no longer the newest official
// release, or that is not newer than the running panel.
type PanelUpdateStaleError struct{ Latest string }

func (e PanelUpdateStaleError) Error() string { return "panel update target is stale: " + e.Latest }

// PanelUpdateRequest is the content of request.json.
type PanelUpdateRequest struct {
	RequestID   string `json:"requestId"`
	Version     string `json:"version"`
	RequestedAt string `json:"requestedAt"`
	RequestedBy string `json:"requestedBy"`
}

// PanelUpdateStatus is the content of status.json as the helper writes it. State
// and code are enums; previousVersion and finishedAt stay empty while the update
// runs. Interrupted is added by the panel and never written to the file: it says that a
// status left in "running" is old enough that the helper which wrote it is gone.
type PanelUpdateStatus struct {
	RequestID       string `json:"requestId"`
	Version         string `json:"version"`
	PreviousVersion string `json:"previousVersion"`
	State           string `json:"state"`
	Code            string `json:"code"`
	StartedAt       string `json:"startedAt"`
	FinishedAt      string `json:"finishedAt"`
	Interrupted     bool   `json:"interrupted"`
}

// PanelUpdatePending is a request the helper has not answered, plus what the card needs
// to know about it. Stalled says the request has waited longer than
// panelUpdateStalledAfter; a stalled request no longer counts as busy, so a new one may
// replace it. It is a separate struct because request.json has no such field.
type PanelUpdatePending struct {
	PanelUpdateRequest
	Stalled bool `json:"stalled"`
}

// PanelUpdateRelease is the newest official GitHub release the lookup found.
type PanelUpdateRelease struct {
	Version     string `json:"version"`
	PublishedAt string `json:"publishedAt"`
	URL         string `json:"url"`
}

// PanelUpdateState is the answer of GET /api/v1/system/panel-update.
type PanelUpdateState struct {
	CurrentVersion    string              `json:"currentVersion"`
	Supported         bool                `json:"supported"`
	UnsupportedReason *string             `json:"unsupportedReason"`
	Latest            *PanelUpdateRelease `json:"latest"`
	LatestError       *string             `json:"latestError"`
	UpdateAvailable   bool                `json:"updateAvailable"`
	Pending           *PanelUpdatePending `json:"pending"`
	Status            *PanelUpdateStatus  `json:"status"`
}

// PanelUpdateService owns the request/status files and the release lookup. The zero
// value is the production wiring: DefaultPanelUpdateDirectory, the public GitHub
// endpoint and a 10 s client; tests replace the fields.
type PanelUpdateService struct {
	// Directory is the helper's request/status directory.
	Directory string
	// CurrentVersion is the running panel release (server.Version). Empty for a
	// plain `go run`, which is a development build.
	CurrentVersion string
	// BaseURL is the "latest release" endpoint.
	BaseURL string
	// Client performs the release lookup.
	Client *http.Client
	// Now is the clock the stalled and interrupted thresholds are measured with. Tests
	// set it; the zero value means time.Now.
	Now func() time.Time

	// mu guards the in-memory cache of the last successful lookup. A failed lookup
	// is never cached, so a retry asks GitHub again.
	mu       sync.Mutex
	cached   *PanelUpdateRelease
	cachedAt time.Time
}

func (s *PanelUpdateService) directory() string {
	if s.Directory == "" {
		return DefaultPanelUpdateDirectory
	}
	return s.Directory
}

func (s *PanelUpdateService) baseURL() string {
	if s.BaseURL == "" {
		return defaultPanelUpdateBaseURL
	}
	return s.BaseURL
}

func (s *PanelUpdateService) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: panelUpdateTimeout}
}

func (s *PanelUpdateService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// parseContractTime reads one RFC3339 timestamp of the contract files.
func parseContractTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// requestStalled reports whether a request has waited longer than
// panelUpdateStalledAfter for the helper to answer it.
func (s *PanelUpdateService) requestStalled(request PanelUpdateRequest) bool {
	requestedAt, ok := parseContractTime(request.RequestedAt)
	return ok && s.now().Sub(requestedAt) > panelUpdateStalledAfter
}

// statusInterrupted reports whether a status left in "running" is older than the helper
// unit's own start timeout, so the helper that wrote it is gone and nothing will finish
// it. A running status without a readable startedAt cannot be trusted the other way.
func (s *PanelUpdateService) statusInterrupted(status PanelUpdateStatus) bool {
	if status.State != panelUpdateRunning {
		return false
	}
	startedAt, ok := parseContractTime(status.StartedAt)
	return !ok || s.now().Sub(startedAt) > panelUpdateInterruptedAfter
}

// unsupportedReason reports why the running panel cannot update itself, or "" when
// it can. An rc build and a development build both lack a release archive, so both
// are "dev_build"; a missing helper directory means the installer never created the
// place where the root helper waits for requests.
func (s *PanelUpdateService) unsupportedReason() string {
	if !officialVersionPattern.MatchString(s.CurrentVersion) {
		return "dev_build"
	}
	info, err := os.Stat(s.directory())
	if err != nil || !info.IsDir() {
		return "helper_missing"
	}
	return ""
}

// compareReleaseVersions orders two official versions: -1 when a is older, 0 when
// equal and 1 when newer. Anything that is not an official version compares equal,
// which never offers an update.
func compareReleaseVersions(a, b string) int {
	left, okLeft := parseReleaseVersion(a)
	right, okRight := parseReleaseVersion(b)
	if !okLeft || !okRight {
		return 0
	}
	for index := range left {
		if left[index] != right[index] {
			if left[index] < right[index] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseReleaseVersion(value string) ([3]int, bool) {
	match := officialVersionPattern.FindStringSubmatch(value)
	if match == nil {
		return [3]int{}, false
	}
	parts := [3]int{}
	for index := range parts {
		number, err := strconv.Atoi(match[index+1])
		if err != nil {
			return [3]int{}, false
		}
		parts[index] = number
	}
	return parts, true
}

// latestRelease answers the newest official release, from the cache while it is
// younger than panelUpdateCacheTTL and refresh is false.
func (s *PanelUpdateService) latestRelease(ctx context.Context, refresh bool) (*PanelUpdateRelease, error) {
	if !refresh {
		s.mu.Lock()
		cached, at := s.cached, s.cachedAt
		s.mu.Unlock()
		if cached != nil && s.now().Sub(at) < panelUpdateCacheTTL {
			return cached, nil
		}
	}
	release, err := s.fetchLatest(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cached, s.cachedAt = release, s.now()
	s.mu.Unlock()
	return release, nil
}

func (s *PanelUpdateService) fetchLatest(ctx context.Context) (*PanelUpdateRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, panelUpdateTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL(), nil)
	if err != nil {
		return nil, ErrPanelUpdateCheckFailed
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", panelUpdateUserAgent)
	response, err := s.client().Do(request)
	if err != nil {
		return nil, ErrPanelUpdateCheckFailed
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrPanelUpdateCheckFailed
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, panelUpdateBodyLimit+1))
	if err != nil || len(data) > panelUpdateBodyLimit {
		return nil, ErrPanelUpdateCheckFailed
	}
	var body struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &body); err != nil || body.Draft || body.Prerelease || body.HTMLURL == "" {
		return nil, ErrPanelUpdateCheckFailed
	}
	// The tag names the release the updater downloads, so only an exact v prefix and
	// an official number is usable.
	if !strings.HasPrefix(body.TagName, "v") || !officialVersionPattern.MatchString(body.TagName[1:]) {
		return nil, ErrPanelUpdateCheckFailed
	}
	return &PanelUpdateRelease{Version: body.TagName[1:], PublishedAt: body.PublishedAt, URL: body.HTMLURL}, nil
}

// State assembles the answer of GET /api/v1/system/panel-update. A release lookup
// failure is reported as the fixed "check_failed" code so no upstream text reaches
// the browser.
func (s *PanelUpdateService) State(ctx context.Context, refresh bool) PanelUpdateState {
	state := PanelUpdateState{CurrentVersion: s.CurrentVersion}
	if reason := s.unsupportedReason(); reason != "" {
		state.UnsupportedReason = &reason
	} else {
		state.Supported = true
	}
	if latest, err := s.latestRelease(ctx, refresh); err != nil {
		code := "check_failed"
		state.LatestError = &code
	} else {
		state.Latest = latest
		state.UpdateAvailable = state.Supported && compareReleaseVersions(latest.Version, s.CurrentVersion) > 0
	}
	state.Status = s.readStatus()
	state.Pending = s.outstandingRequest(state.Status)
	return state
}

// outstandingRequest reports the request the helper has not answered yet, or nil when
// there is none. The rule is the same one GET and POST need: no readable request.json,
// or a status.json that names this very request id, means nothing is outstanding. The
// helper never deletes request.json, so after a finished update the file still names the
// request while only status.json says the work is done; treating that file as busy would
// refuse every later submission for panelUpdateStalledAfter and would disagree with the
// pending request GET reports. A stalled request is still outstanding - nothing answered
// it - but it no longer blocks a new one, which is why the flag travels with it.
func (s *PanelUpdateService) outstandingRequest(status *PanelUpdateStatus) *PanelUpdatePending {
	request, ok := s.readRequest()
	if !ok {
		return nil
	}
	if status != nil && status.RequestID == request.RequestID {
		return nil
	}
	return &PanelUpdatePending{PanelUpdateRequest: request, Stalled: s.requestStalled(request)}
}

// Submit writes the update request for the helper after checking, in order, that
// the panel can update itself, that no request is outstanding, that the latest
// official release can still be read, and that the requested version is exactly
// that release and newer than the running panel. A request the helper has already
// answered - status.json names its id, whatever state that run ended in - is not
// outstanding, and a stalled request or an interrupted run is not busy: nothing is
// working on them, so the new request replaces the file instead of failing.
func (s *PanelUpdateService) Submit(ctx context.Context, version, actor string) (PanelUpdateRequest, error) {
	if reason := s.unsupportedReason(); reason != "" {
		return PanelUpdateRequest{}, PanelUpdateUnsupportedError{Reason: reason}
	}
	status := s.readStatus()
	if pending := s.outstandingRequest(status); pending != nil && !pending.Stalled {
		return PanelUpdateRequest{}, ErrPanelUpdateBusy
	}
	if status != nil && status.State == panelUpdateRunning && !status.Interrupted {
		return PanelUpdateRequest{}, ErrPanelUpdateBusy
	}
	latest, err := s.latestRelease(ctx, false)
	if err != nil {
		return PanelUpdateRequest{}, ErrPanelUpdateCheckFailed
	}
	if version != latest.Version || compareReleaseVersions(latest.Version, s.CurrentVersion) <= 0 {
		return PanelUpdateRequest{}, PanelUpdateStaleError{Latest: latest.Version}
	}
	id, err := newPanelUpdateRequestID()
	if err != nil {
		return PanelUpdateRequest{}, err
	}
	request := PanelUpdateRequest{RequestID: id, Version: version, RequestedAt: s.now().UTC().Format(time.RFC3339), RequestedBy: actor}
	if err := writePanelUpdateFile(s.directory(), panelUpdateRequestFile, request); err != nil {
		return PanelUpdateRequest{}, err
	}
	return request, nil
}

func newPanelUpdateRequestID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// writePanelUpdateFile writes one contract file atomically: the helper never sees a
// half-written request, and the mode is fixed because the file names the version
// that will replace the panel.
func writePanelUpdateFile(directory, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	handle, err := os.CreateTemp(directory, name+".tmp-*")
	if err != nil {
		return err
	}
	temporary := handle.Name()
	defer os.Remove(temporary)
	if _, err := handle.Write(data); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Sync(); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Chmod(0600); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(directory, name))
}

// readContractFile reads one contract file under panelUpdateFileLimit. A missing file
// and one that cannot be read both answer ok=false: the caller cannot tell a first
// run from an unreadable helper file and treats both as "no state".
func (s *PanelUpdateService) readContractFile(name string) ([]byte, bool) {
	file, err := os.Open(filepath.Join(s.directory(), name))
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, panelUpdateFileLimit+1))
	if err != nil || len(data) > panelUpdateFileLimit {
		return nil, false
	}
	return data, true
}

// readRequest reports the request written for the helper. Anything that does not
// match the contract is ignored: a corrupt request must not make the panel look
// busy forever.
func (s *PanelUpdateService) readRequest() (PanelUpdateRequest, bool) {
	data, ok := s.readContractFile(panelUpdateRequestFile)
	if !ok {
		return PanelUpdateRequest{}, false
	}
	var request PanelUpdateRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return PanelUpdateRequest{}, false
	}
	if !panelUpdateRequestIDPattern.MatchString(request.RequestID) || !officialVersionPattern.MatchString(request.Version) {
		return PanelUpdateRequest{}, false
	}
	if _, ok := parseContractTime(request.RequestedAt); !ok {
		return PanelUpdateRequest{}, false
	}
	return request, true
}

// readStatus reports the helper's last status.json. Unknown keys are tolerated (a
// newer helper may add fields) but the state and code must be known values and the
// id and version must match the contract; anything else is no status at all. The
// derived Interrupted flag is filled in here so every caller sees the same answer.
func (s *PanelUpdateService) readStatus() *PanelUpdateStatus {
	data, ok := s.readContractFile(panelUpdateStatusFile)
	if !ok {
		return nil
	}
	var status PanelUpdateStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil
	}
	if !panelUpdateRequestIDPattern.MatchString(status.RequestID) || !officialVersionPattern.MatchString(status.Version) {
		return nil
	}
	if status.State != panelUpdateRunning && status.State != panelUpdateSucceeded && status.State != panelUpdateFailed {
		return nil
	}
	if !panelUpdateStatusCodes[status.Code] {
		return nil
	}
	status.Interrupted = s.statusInterrupted(status)
	return &status
}
