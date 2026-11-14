package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/subscription/source"
)

// The refresh handler maps every business outcome onto its own status code and
// keeps the bounded result in the error body, so the page can show counts and
// reasons instead of a bare message.
func TestRefreshStatusMapping(t *testing.T) {
	const content = "socks5://fixture:private-fixture@example.com:1080#cached"
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantCode   string
		wantResult string
	}{
		{"success", http.StatusOK, content, http.StatusOK, "", "import_ok"},
		{"partial", http.StatusOK, content + "\nvmess://unsupported", http.StatusOK, "", "import_partial"},
		{"http status", http.StatusBadGateway, "upstream failure", http.StatusBadGateway, "import_refresh_failed", "source_http_status"},
		{"no entries", http.StatusOK, "hello, this is not a subscription", http.StatusUnprocessableEntity, "import_no_entries", "import_unrecognized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, userID, _ := storeFixture(t)
			ctx := context.Background()
			s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture?token=private-fixture", Interval: 60})
			if err != nil {
				t.Fatal(err)
			}
			grantImport(t, s, userID, key, "")
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.SetPathValue("id", key)
			w := httptest.NewRecorder()
			(&Handler{Store: s}).refresh(w, r)
			if w.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, test.wantStatus, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private-fixture") {
				t.Fatal("refresh response leaked the source")
			}
			if test.wantCode != "" {
				var response struct {
					Error struct {
						Code   string `json:"code"`
						Result Result `json:"result"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Error.Code != test.wantCode || response.Error.Result.Code != test.wantResult || response.Error.Result.AttemptedAt == "" {
					t.Fatalf("error body: %s", w.Body.String())
				}
			} else {
				var result Result
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Code != test.wantResult || result.Outcome == "" || result.AttemptedAt == "" {
					t.Fatalf("success body: %s", w.Body.String())
				}
			}
			items, err := s.List(ctx)
			if err != nil || len(items) != 1 {
				t.Fatalf("list: %v, number=%d", err, len(items))
			}
			proxies, warnings, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}})
			if err != nil || len(warnings) == 0 {
				t.Fatalf("subscription read: %v", err)
			}
			wantStatus, wantProxies := "ok", 1
			if test.wantStatus != http.StatusOK {
				wantStatus, wantProxies = "failed", 0
			}
			if items[0].Status != wantStatus || len(proxies) != wantProxies {
				t.Fatalf("source status %q with %d proxies, want %q/%d", items[0].Status, len(proxies), wantStatus, wantProxies)
			}
		})
	}
}

// A failed attempt keeps the previous cache for diagnostics but excludes it from
// subscriptions, and a commit failure leaves both the cache and the status as they
// were. The result always describes the state that remains, never the one the
// attempt hoped for.
func TestRefreshFailureKeepsCacheAndCommitFailureRollsBack(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	const content = "socks5://fixture:fixture@example.com:1080#cached"
	status, body := http.StatusOK, content
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, key, "")
	first, err := s.Refresh(ctx, key)
	if err != nil || first.Outcome != "ok" || *first.Counts.Cached != 1 || *first.Counts.Usable != 1 {
		t.Fatalf("first refresh: %+v %v", first, err)
	}
	// An upstream failure records the cause, keeps the rows and disables them.
	status, body = http.StatusBadGateway, "upstream failure"
	failed, err := s.Refresh(ctx, key)
	if !errors.Is(err, ErrFetch) || failed.Code != "source_http_status" || *failed.Counts.Cached != 1 || *failed.Counts.Usable != 0 {
		t.Fatalf("failure result: %+v %v", failed, err)
	}
	if failed.Status == nil || *failed.Status != http.StatusBadGateway || !strings.Contains(failed.Message, "HTTP 502") {
		t.Fatalf("upstream status lost: %+v", failed)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	if items[0].Status != "failed" || items[0].LastSuccess == "" || items[0].LatestResult == nil || items[0].LatestResult.Code != "source_http_status" {
		t.Fatalf("source after failure: %+v", items[0])
	}
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 0 {
		t.Fatalf("failed source still contributed %d endpoints", len(p))
	}
	// A successful fetch whose commit fails changes nothing, so the result keeps the
	// recorded status: the source is still failed and its cache is still excluded.
	status, body = http.StatusOK, content
	abortOn(t, s, "reject_refresh_commit", "BEFORE UPDATE OF last_status ON subscription_sources WHEN NEW.last_status='ok'")
	committed, err := s.Refresh(ctx, key)
	if err == nil || errors.Is(err, ErrFetch) || errors.Is(err, ErrParse) || !strings.Contains(err.Error(), "fixture abort") {
		t.Fatalf("persistence error hidden: %v", err)
	}
	if committed.Outcome != "failed" || committed.Stage != stageCommit || committed.Code != "import_commit_failed" ||
		committed.Counts.Accepted != 0 || committed.Counts.Total != nil || committed.Counts.Rejected != nil ||
		*committed.Counts.Cached != 1 || *committed.Counts.Usable != 0 {
		t.Fatalf("commit failure result: %+v", committed)
	}
	if committed.LastSuccessAt == nil || *committed.LastSuccessAt != items[0].LastSuccess || committed.AttemptedAt == *committed.LastSuccessAt {
		t.Fatalf("commit failure reported a new success: %+v", committed)
	}
	if raw, marshalErr := json.Marshal(committed); marshalErr != nil || ValidateResult(raw) != nil {
		t.Fatalf("commit failure result is not panel-shaped: %+v %v", committed, marshalErr)
	}
	items, err = s.List(ctx)
	if err != nil || items[0].Status != "failed" || items[0].LatestResult.Code != "source_http_status" {
		t.Fatalf("failed commit changed the source: %+v %v", items, err)
	}
	assertCache(t, s, key, 1, "cached")
	// Dropping the trigger lets the next attempt recover normally.
	dropTrigger(t, s, "reject_refresh_commit")
	if first, err = s.Refresh(ctx, key); err != nil || first.Outcome != "ok" || *first.Counts.Cached != 1 {
		t.Fatalf("recovery: %+v %v", first, err)
	}
	items, err = s.List(ctx)
	if err != nil || items[0].Status != "ok" || items[0].Error != "" || items[0].LatestResult.Outcome != "ok" {
		t.Fatalf("recovered source: %+v %v", items, err)
	}
}

// A new URL is a new source: the pending state must not present the previous
// attempt, or its cached endpoints, as this source's result.
func TestUrlChangeClearsPreviousResult(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#cached")
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, key, "")
	if _, err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Save(ctx, key, Input{Name: "fixture", URL: "https://example.com/next", Interval: 60}); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	if items[0].Status != "pending" || items[0].LatestResult != nil || items[0].LastSuccess != "" || items[0].LastFetch != "" {
		t.Fatalf("pending source kept a previous result: %+v", items[0])
	}
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 0 {
		t.Fatalf("pending source contributed %d endpoints", len(p))
	}
}

// abortOn installs a trigger that refuses one write, so a persistence failure can be
// injected at a chosen statement.
func abortOn(t *testing.T, s *Store, name, when string) {
	t.Helper()
	if _, err := s.Database.Exec("CREATE TRIGGER " + name + " " + when + " BEGIN SELECT RAISE(ABORT, 'fixture abort'); END"); err != nil {
		t.Fatal(err)
	}
}

func dropTrigger(t *testing.T, s *Store, name string) {
	t.Helper()
	if _, err := s.Database.Exec("DROP TRIGGER " + name); err != nil {
		t.Fatal(err)
	}
}

// assertCache checks the source's stored endpoints and the name of the only one.
func assertCache(t *testing.T, s *Store, key string, count int, name string) {
	t.Helper()
	var cached int
	if err := s.Database.QueryRow("SELECT count(*) FROM imported_proxies WHERE source_id=?", key).Scan(&cached); err != nil || cached != count {
		t.Fatalf("cache holds %d endpoints, want %d: %v", cached, count, err)
	}
	var stored string
	if err := s.Database.QueryRow("SELECT name FROM imported_proxies WHERE source_id=? ORDER BY id LIMIT 1", key).Scan(&stored); err != nil || stored != name {
		t.Fatalf("cached endpoint %q, want %q: %v", stored, name, err)
	}
}

// refreshHTTP runs the real refresh route.
func refreshHTTP(s *Store, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.SetPathValue("id", key)
	w := httptest.NewRecorder()
	(&Handler{Store: s}).refresh(w, r)
	return w
}

// errorResult decodes the error body of a refused attempt.
func errorResult(t *testing.T, w *httptest.ResponseRecorder) (string, string, Result) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Result  Result `json:"result"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body: %v", err)
	}
	return body.Error.Code, body.Error.Message, body.Error.Result
}

// A storage failure after a successful commit reports the state the source keeps: the
// previous success time and a cache that is still usable, never the endpoints this
// attempt analysed. The route answers 500 with the panel's own text and result.
func TestRefreshCommitFailureKeepsPreviousSuccess(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	body := "socks5://fixture:fixture@example.com:1080#first"
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, key, "")
	if first, err := s.Refresh(ctx, key); err != nil || first.Outcome != "ok" || *first.Counts.Cached != 1 {
		t.Fatalf("first refresh: %+v %v", first, err)
	}
	items, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous := items[0].LastSuccess
	// The next attempt fetches two different endpoints, and its commit fails.
	body = "socks5://fixture:fixture@example.com:1081#second\nsocks5://fixture:fixture@example.com:1082#third"
	abortOn(t, s, "reject_refresh_commit", "BEFORE UPDATE OF last_status ON subscription_sources WHEN NEW.last_status='ok'")
	committed, err := s.Refresh(ctx, key)
	if err == nil || errors.Is(err, ErrFetch) || errors.Is(err, ErrParse) || !strings.Contains(err.Error(), "fixture abort") {
		t.Fatalf("persistence error hidden: %v", err)
	}
	if committed.Outcome != "failed" || committed.Stage != stageCommit || committed.Code != "import_commit_failed" {
		t.Fatalf("commit failure result: %+v", committed)
	}
	if committed.Counts.Accepted != 0 || committed.Counts.Total != nil || committed.Counts.Cached == nil ||
		*committed.Counts.Cached != 1 || committed.Counts.Usable == nil || *committed.Counts.Usable != 1 {
		t.Fatalf("commit failure lied about the cache: %+v", committed.Counts)
	}
	if committed.LastSuccessAt == nil || *committed.LastSuccessAt != previous || committed.AttemptedAt == previous {
		t.Fatalf("commit failure reported a new success: %+v", committed)
	}
	if raw, marshalErr := json.Marshal(committed); marshalErr != nil || ValidateResult(raw) != nil {
		t.Fatalf("commit failure result is not panel-shaped: %+v %v", committed, marshalErr)
	}
	items, err = s.List(ctx)
	if err != nil || items[0].Status != "ok" || items[0].LastSuccess != previous || items[0].LatestResult.Outcome != "ok" {
		t.Fatalf("failed commit changed the source: %+v %v", items, err)
	}
	assertCache(t, s, key, 1, "first")
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 1 {
		t.Fatalf("a rolled back commit changed the subscription: %d endpoints, %v", len(p), err)
	}
	w := refreshHTTP(s, key)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status code %d: %s", w.Code, w.Body.String())
	}
	code, message, reported := errorResult(t, w)
	if code != "import_commit_failed" || message != "导入结果未能保存，来源状态保持不变" {
		t.Fatalf("error body: %s", w.Body.String())
	}
	if reported.Counts.Usable == nil || *reported.Counts.Usable != 1 || reported.Counts.Accepted != 0 {
		t.Fatalf("reported counts: %+v", reported.Counts)
	}
	for _, leaked := range []string{"fixture abort", "HTTP 0", "source_http_status"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Fatalf("response leaked %q: %s", leaked, w.Body.String())
		}
	}
}

// A failure while replacing the cache rolls the whole attempt back, and the old
// endpoints stay usable.
func TestRefreshReplaceFailureReportsCommit(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#first")
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	abortOn(t, s, "reject_refresh_replace", "BEFORE DELETE ON imported_proxies")
	result, err := s.Refresh(ctx, key)
	if err == nil || errors.Is(err, ErrFetch) || errors.Is(err, ErrParse) || !strings.Contains(err.Error(), "fixture abort") {
		t.Fatalf("persistence error hidden: %v", err)
	}
	if result.Stage != stageCommit || result.Code != "import_commit_failed" || *result.Counts.Cached != 1 || *result.Counts.Usable != 1 {
		t.Fatalf("replace failure result: %+v", result)
	}
	assertCache(t, s, key, 1, "first")
}

// A failure that cannot be recorded keeps the previous status, so the cache stays
// usable and the result says exactly that.
func TestRefreshRecordFailureWriteKeepsState(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	status := http.StatusOK
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#first")
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, key, "")
	if _, err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	status = http.StatusForbidden
	abortOn(t, s, "reject_refresh_record", "BEFORE UPDATE OF last_status ON subscription_sources WHEN NEW.last_status='failed'")
	result, err := s.Refresh(ctx, key)
	if err == nil || errors.Is(err, ErrFetch) || errors.Is(err, ErrParse) || !strings.Contains(err.Error(), "fixture abort") {
		t.Fatalf("persistence error hidden: %v", err)
	}
	if result.Stage != stageCommit || result.Code != "import_commit_failed" || result.Status != nil {
		t.Fatalf("record failure result: %+v", result)
	}
	if *result.Counts.Cached != 1 || *result.Counts.Usable != 1 {
		t.Fatalf("record failure lied about the cache: %+v", result.Counts)
	}
	items, err := s.List(ctx)
	if err != nil || items[0].Status != "ok" || items[0].LatestResult.Outcome != "ok" {
		t.Fatalf("a rolled back failure record changed the source: %+v %v", items, err)
	}
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 1 {
		t.Fatalf("a rolled back failure record disabled the cache: %d endpoints, %v", len(p), err)
	}
}

// An attempt that cannot even start its commit transaction reports a commit failure
// with the state the source keeps.
func TestRefreshBeginFailureReportsCommit(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#first")
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	// Hold the only writer connection: the commit transaction cannot start. Reads keep
	// working through the read pool, as they do in the panel.
	held, err := s.Database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	run, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.Refresh(run, key)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("begin failure: %v", err)
	}
	if result.Outcome != "failed" || result.Stage != stageCommit || result.Code != "import_commit_failed" {
		t.Fatalf("begin failure result: %+v", result)
	}
	if result.Counts.Accepted != 0 || *result.Counts.Cached != 1 || *result.Counts.Usable != 1 {
		t.Fatalf("begin failure lied about the cache: %+v", result.Counts)
	}
}

// The real upstream status reaches the result, the stored row and the response, so a
// 403 is never reported as HTTP 0.
func TestRefreshReportsUpstreamStatus(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture?token=private-fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Refresh(ctx, key)
	if !errors.Is(err, ErrFetch) || result.Status == nil || *result.Status != http.StatusForbidden {
		t.Fatalf("upstream status lost: %+v %v", result, err)
	}
	if !strings.Contains(result.Message, "HTTP 403") || strings.Contains(result.Message, "HTTP 0") {
		t.Fatalf("message: %q", result.Message)
	}
	var stored, lastFetch, lastSuccess, status string
	if err = s.Database.QueryRow("SELECT latest_result_json,last_fetch_at,last_success_at,last_status FROM subscription_sources WHERE id=?", key).Scan(&stored, &lastFetch, &lastSuccess, &status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(stored, `"upstreamStatus":403`) {
		t.Fatalf("stored row: %s %s", status, stored)
	}
	if err = ValidateStoredResult(key, lastFetch, lastSuccess, status, []byte(stored)); err != nil {
		t.Fatalf("stored result rejected: %v", err)
	}
	w := refreshHTTP(s, key)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status code %d: %s", w.Code, w.Body.String())
	}
	code, message, reported := errorResult(t, w)
	if code != "import_refresh_failed" || !strings.Contains(message, "HTTP 403") {
		t.Fatalf("error body: %s", w.Body.String())
	}
	if reported.Status == nil || *reported.Status != http.StatusForbidden {
		t.Fatalf("reported status: %+v", reported.Status)
	}
	for _, leaked := range []string{"HTTP 0", "private-fixture"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Fatalf("response leaked %q: %s", leaked, w.Body.String())
		}
	}
}

// A source whose own sealed payload cannot be opened is a local failure, never a
// fetch: the panel answers 500 with fixed text, writes nothing, reports the cache the
// source keeps, and never echoes the vault error or the source's secret.
func TestRefreshUnreadablePayloadIsNotAFetch(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#cached")
	})
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture?token=private-fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, key, "")
	if _, err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %+v %v", items, err)
	}
	before := items[0]
	// Corrupt only the sealed link, as a damaged vault or a foreign key would.
	var sealedURL []byte
	if err = s.Database.QueryRow("SELECT url_encrypted FROM subscription_sources WHERE id=?", key).Scan(&sealedURL); err != nil {
		t.Fatal(err)
	}
	beforeCache := identitySnapshot(t, s, key, false)
	if _, err = s.Database.Exec("UPDATE subscription_sources SET url_encrypted=x'00' WHERE id=?", key); err != nil {
		t.Fatal(err)
	}
	result, err := s.Refresh(ctx, key)
	if !errors.Is(err, ErrStore) || errors.Is(err, ErrFetch) {
		t.Fatalf("an unreadable payload was reported as a fetch: %v", err)
	}
	if result.Outcome != "failed" || result.Stage != stageStore || result.Code != "import_source_unreadable" ||
		result.Message != resultMessage("import_source_unreadable", nil) || result.Message == "" {
		t.Fatalf("storage result: %+v", result)
	}
	if result.Counts.Accepted != 0 || result.Counts.Cached == nil || *result.Counts.Cached != 1 ||
		result.Counts.Usable == nil || *result.Counts.Usable != 1 {
		t.Fatalf("storage result lied about the cache: %+v", result.Counts)
	}
	if result.LastSuccessAt == nil || *result.LastSuccessAt != before.LastSuccess || result.AttemptedAt == "" {
		t.Fatalf("storage result changed the success time: %+v", result)
	}
	if raw, marshalErr := json.Marshal(result); marshalErr != nil || ValidateResult(raw) != nil {
		t.Fatalf("storage result is not panel-shaped: %+v %v", result, marshalErr)
	}
	// Nothing was written, so the source, its result and its endpoints are unchanged.
	after, err := s.List(ctx)
	if err != nil || len(after) != 1 || after[0].Status != before.Status || after[0].LastSuccess != before.LastSuccess ||
		after[0].LastFetch != before.LastFetch || after[0].LatestResult == nil || after[0].LatestResult.Code != "import_ok" {
		t.Fatalf("a storage failure changed the source: %+v %v", after, err)
	}
	if afterCache := identitySnapshot(t, s, key, false); afterCache != beforeCache {
		t.Fatal("a storage failure changed the cached endpoints")
	}
	if _, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err == nil || !strings.Contains(err.Error(), "invalid encrypted configuration") {
		t.Fatalf("identity resolution hid the unreadable source kind: %v", err)
	}
	// The answer is the panel's own 500 with the same result, never the vault error.
	w := refreshHTTP(s, key)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	code, message, reported := errorResult(t, w)
	if code != "import_source_unreadable" || message != resultMessage("import_source_unreadable", nil) ||
		reported.Code != "import_source_unreadable" || reported.AttemptedAt == "" || reported.Message != message {
		t.Fatalf("error body: %s", w.Body.String())
	}
	lowered := strings.ToLower(w.Body.String())
	for _, leaked := range []string{"private-fixture", "vault", "cipher", "gcm", "decrypt", "nonce"} {
		if strings.Contains(lowered, leaked) {
			t.Fatalf("response leaked %q: %s", leaked, w.Body.String())
		}
	}
	// With the original source identity restored, the unchanged cache is usable.
	if _, err = s.Database.Exec("UPDATE subscription_sources SET url_encrypted=? WHERE id=?", sealedURL, key); err != nil {
		t.Fatal(err)
	}
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 1 {
		t.Fatalf("a storage failure changed the served endpoints: %d %v", len(p), err)
	}
}

// A request cancelled while the fetch is still running did not finish: nothing is
// classified, so the panel answers with its own 503 and no provisional result, and
// the source keeps exactly the row and cache it had. Returning the attempt so far
// would show the Owner empty counts, a missing reason list and a message that
// replaces the truthful cancellation text.
func TestRefreshCancelledDuringFetchKeepsTheSourceAndSendsNoResult(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	fetcher, _, _ := fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#cached")
	})
	s.Fetcher = fetcher
	key, _, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture?token=private-fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, key, "")
	if _, err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 || items[0].LatestResult == nil || items[0].LatestResult.Code != "import_ok" {
		t.Fatalf("committed source: %+v %v", items, err)
	}
	before := items[0]
	// The upstream now stalls until its caller gives up, as a slow source does: the
	// dial blocks on the request's own context instead of a timer.
	started := make(chan struct{})
	s.Fetcher.DialContext = func(dialCtx context.Context, _, _ string) (net.Conn, error) {
		close(started)
		<-dialCtx.Done()
		return nil, dialCtx.Err()
	}
	request, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(request)
	r.SetPathValue("id", key)
	w := httptest.NewRecorder()
	served := make(chan struct{})
	go func() {
		defer close(served)
		(&Handler{Store: s}).refresh(w, r)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh never reached the fetch")
	}
	cancel()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh did not stop after the cancellation")
	}
	// The panel's own 503 with its fixed text and no result attached.
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Result  json.RawMessage `json:"result"`
		} `json:"error"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body: %v", err)
	}
	if body.Error.Code != "import_unavailable" || body.Error.Message != "刷新已取消，来源状态保持不变" {
		t.Fatalf("error body: %s", w.Body.String())
	}
	if len(body.Error.Result) != 0 {
		t.Fatalf("a cancelled refresh carried a result: %s", w.Body.String())
	}
	for _, leaked := range []string{"attemptedAt", "diagnostics", "counts", "private-fixture"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Fatalf("cancelled refresh carried %q: %s", leaked, w.Body.String())
		}
	}
	// Nothing was written: the row, its last result and the served cache are the same.
	after, err := s.List(ctx)
	if err != nil || len(after) != 1 || after[0].Status != before.Status || after[0].LastFetch != before.LastFetch ||
		after[0].LastSuccess != before.LastSuccess || after[0].LatestResult == nil || after[0].LatestResult.Code != "import_ok" {
		t.Fatalf("a cancelled refresh changed the source: %+v %v", after, err)
	}
	assertCache(t, s, key, 1, "cached")
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 1 {
		t.Fatalf("a cancelled refresh changed the served endpoints: %d %v", len(p), err)
	}
}
