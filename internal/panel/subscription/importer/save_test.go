package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/source"
)

// saveHTTP runs the real save route with one JSON input.
func saveHTTP(s *Store, key string, in Input) *httptest.ResponseRecorder {
	raw, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.SetPathValue("id", key)
	}
	w := httptest.NewRecorder()
	(&Handler{Store: s}).save(w, r)
	return w
}

// savedSource decodes the success body of the save route.
func savedSource(t *testing.T, w *httptest.ResponseRecorder) (string, *Result) {
	t.Helper()
	var body struct {
		ID     string  `json:"id"`
		Result *Result `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("save body: %v", err)
	}
	return body.ID, body.Result
}

// A pasted import shares the fetch pipeline: the attempt's bounded result comes back
// with the stored source, so a mixed paste reports what it kept and an unusable paste
// answers 422 with its per-entry reasons instead of a bare message.
func TestSavePasteReturnsBoundedResult(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	mixed := saveHTTP(s, "", Input{Name: "mixed paste", Interval: 60, Content: "socks5://fixture:fixture@example.com:1080#kept\nvmess://unsupported.example.com:443"})
	if mixed.Code != http.StatusOK {
		t.Fatalf("mixed paste: %d %s", mixed.Code, mixed.Body.String())
	}
	key, result := savedSource(t, mixed)
	if key == "" || result == nil {
		t.Fatalf("save body: %s", mixed.Body.String())
	}
	if result.Outcome != "partial" || result.Code != "import_partial" || result.Counts.Accepted != 1 ||
		result.Counts.Cached == nil || *result.Counts.Cached != 1 || result.Counts.Usable == nil || *result.Counts.Usable != 1 {
		t.Fatalf("mixed result: %+v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unsupported_protocol" {
		t.Fatalf("mixed diagnostics: %+v", result.Diagnostics)
	}
	if result.Message != resultMessage(result.Code, nil) {
		t.Fatalf("mixed message: %q", result.Message)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 || items[0].Status != "ok" || items[0].LatestResult == nil || items[0].LatestResult.Outcome != "partial" {
		t.Fatalf("stored paste: %+v %v", items, err)
	}
	var stored, lastFetch, lastSuccess, status string
	if err = s.Database.QueryRow("SELECT latest_result_json,last_fetch_at,last_success_at,last_status FROM subscription_sources WHERE id=?", key).Scan(&stored, &lastFetch, &lastSuccess, &status); err != nil {
		t.Fatal(err)
	}
	if err = ValidateStoredResult(key, lastFetch, lastSuccess, status, []byte(stored)); err != nil {
		t.Fatalf("stored paste rejected: %v", err)
	}
	// A clean paste reports a plain success and keeps the id-only shape.
	clean := saveHTTP(s, "", Input{Name: "clean paste", Interval: 60, Content: "socks5://fixture:fixture@example.com:1081#clean"})
	if clean.Code != http.StatusOK {
		t.Fatalf("clean paste: %d %s", clean.Code, clean.Body.String())
	}
	if _, cleanResult := savedSource(t, clean); cleanResult == nil || cleanResult.Outcome != "ok" || cleanResult.Counts.Accepted != 1 {
		t.Fatalf("clean result: %+v", cleanResult)
	}
	// A source saved with a link has no pasted attempt, so it carries no result.
	linked := saveHTTP(s, "", Input{Name: "linked", Interval: 60, URL: "https://example.com/fixture"})
	if linked.Code != http.StatusOK {
		t.Fatalf("linked save: %d %s", linked.Code, linked.Body.String())
	}
	if _, linkedResult := savedSource(t, linked); linkedResult != nil {
		t.Fatalf("linked save reported an attempt: %+v", linkedResult)
	}
}

// An unusable paste answers 422 with the same bounded result the list would show, and
// nothing is stored. The result states that no source was created: it never claims a
// fetch or an exclusion that did not happen.
func TestSaveUnusablePasteReturnsResult(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	w := saveHTTP(s, "", Input{Name: "unsupported paste", Interval: 60, Content: "vmess://a.example.com:443\nvmess://b.example.com:443"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	code, message, result := errorResult(t, w)
	// The HTTP code names the refused document; the result names the disposition.
	if code != "import_no_entries" || message != resultMessage("import_paste_rejected", nil) {
		t.Fatalf("error body: %s", w.Body.String())
	}
	if result.Outcome != "failed" || result.Stage != stageParse || result.Code != "import_paste_rejected" ||
		result.Counts.Accepted != 0 || result.Counts.Total == nil || *result.Counts.Total != 2 || result.Counts.Cached != nil || result.Counts.Usable != nil {
		t.Fatalf("unusable result: %+v", result)
	}
	if len(result.Diagnostics) != 2 || result.AttemptedAt == "" {
		t.Fatalf("unusable diagnostics: %+v", result)
	}
	if strings.Contains(result.Message, "已从订阅中排除") || strings.Contains(result.Message, "已抓取") {
		t.Fatalf("a refused paste claimed a fetch: %q", result.Message)
	}
	if raw, err := json.Marshal(result); err != nil || ValidateResult(raw) != nil {
		t.Fatalf("refused paste result is not panel-shaped: %+v %v", result, err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("a refused paste stored a source: %+v %v", items, err)
	}
	var stored int
	if err = s.Database.QueryRow("SELECT count(*) FROM imported_proxies").Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("a refused paste stored endpoints: %d %v", stored, err)
	}
	// The same document pasted from a Base64 body is classified, not invented.
	if w = saveHTTP(s, "", Input{Name: "base64 paste", Interval: 60, Content: "QUJDRA==+QUJDRA=="}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("base64 status %d: %s", w.Code, w.Body.String())
	}
	if _, _, result = errorResult(t, w); result.Code != "import_paste_rejected" || result.Stage != stageDecode {
		t.Fatalf("base64 result: %+v", result)
	}
}

// Existing sources refuse batch replacement or a kind switch before analysis.
// The response has no attempt result, and the source and its usable cache stay intact.
func TestSaveRejectedPasteKeepsExistingSource(t *testing.T) {
	for _, kind := range []string{"content", "url"} {
		t.Run(kind, func(t *testing.T) {
			s, userID, _ := storeFixture(t)
			ctx := context.Background()
			in := Input{Name: "existing", Interval: 60, Content: "socks5://fixture:fixture@example.com:1080#kept"}
			if kind == "url" {
				s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "socks5://fixture:fixture@example.com:1080#kept")
				})
				in = Input{Name: "existing", Interval: 60, URL: "https://example.com/fixture"}
			}
			key, first, err := s.Save(ctx, "", in)
			if err != nil {
				t.Fatalf("source: %v", err)
			}
			// A pasted source commits at once; a linked source commits on its first
			// successful refresh.
			if kind == "content" && first.Outcome != "ok" {
				t.Fatalf("pasted source: %+v", first)
			}
			if kind == "url" {
				if first, err = s.Refresh(ctx, key); err != nil || first.Outcome != "ok" {
					t.Fatalf("first refresh: %+v %v", first, err)
				}
			}
			grantImport(t, s, userID, key, "")
			before := identitySnapshot(t, s, key, true)
			// Even an unsupported paste is rejected as a forbidden source operation.
			w := saveHTTP(s, key, Input{Name: "existing", Interval: 60, Content: "vmess://unsupported.example.com:443"})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			wantMessage := "单独导入不支持整批替换，请新增导入节点"
			if kind == "url" {
				wantMessage = "不能切换导入来源类型，请新增导入来源"
			}
			code, message, _ := errorResult(t, w)
			if code != "import_invalid_input" || message != wantMessage {
				t.Fatalf("error body: %s", w.Body.String())
			}
			if strings.Contains(w.Body.String(), `"result"`) {
				t.Fatalf("a refused source operation claimed an attempt: %s", w.Body.String())
			}
			// The row, its cache and its source kind are untouched, so its endpoints
			// still serve a subscription.
			if after := identitySnapshot(t, s, key, true); after != before {
				t.Fatalf("a refused paste changed the source: %+v -> %+v", before, after)
			}
			assertCache(t, s, key, 1, "kept")
			if p, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 1 {
				t.Fatalf("a refused paste changed the served endpoints: %d %v", len(p), err)
			}
		})
	}
}

// Invalid input is a 400 before any attempt, so the answer carries no result; an
// invalid link still produces the panel's fixed text.
func TestSaveRejectsInvalidInput(t *testing.T) {
	s, _, _ := storeFixture(t)
	for _, test := range []struct {
		name  string
		input Input
	}{
		{"empty name", Input{Interval: 60, Content: "socks5://fixture:fixture@example.com:1080#A"}},
		{"short interval", Input{Name: "x", Interval: 10, Content: "socks5://fixture:fixture@example.com:1080#A"}},
		{"long interval", Input{Name: "x", Interval: 2592001, Content: "socks5://fixture:fixture@example.com:1080#A"}},
		{"both link and content", Input{Name: "x", Interval: 60, URL: "https://example.com/fixture", Content: "ss://x"}},
		{"nothing at all", Input{Name: "x", Interval: 60}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := saveHTTP(s, "", test.input)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), `"result"`) {
				t.Fatalf("an input error carried a result: %s", w.Body.String())
			}
			if code, _, _ := errorResult(t, w); code != "import_invalid_input" {
				t.Fatalf("error code: %s", w.Body.String())
			}
		})
	}
	w := saveHTTP(s, "", Input{Name: "x", Interval: 60, URL: "http://example.com/fixture"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid link status %d: %s", w.Code, w.Body.String())
	}
	if code, message, result := errorResult(t, w); code != "import_invalid_input" || message != resultMessage("invalid_source_url", nil) ||
		result.Stage != stageInput || result.Code != "invalid_source_url" {
		t.Fatalf("invalid link body: %s", w.Body.String())
	}
	// An unknown id is a missing source, not invalid input.
	if w = saveHTTP(s, "missing", Input{Name: "x", Interval: 60, Content: "socks5://fixture:fixture@example.com:1080#A"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing source status %d: %s", w.Code, w.Body.String())
	}
}

// A new paste whose write fails reports 500, leaves no partial source or nodes,
// and preserves existing sources. Storage error text never reaches the response.
func TestSavePersistenceFailureKeepsState(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	key, first, err := s.Save(ctx, "", Input{Name: "paste", Interval: 60, Content: "socks5://fixture:fixture@example.com:1080#first"})
	if err != nil || first.Outcome != "ok" {
		t.Fatalf("first paste: %+v %v", first, err)
	}
	before := identitySnapshot(t, s, key, true)
	abortOn(t, s, "reject_save_commit", "BEFORE UPDATE OF last_status ON subscription_sources WHEN NEW.last_status='ok'")
	w := saveHTTP(s, "", Input{Name: "new paste", Interval: 60, Content: "socks5://fixture:fixture@example.com:1081#second\nsocks5://fixture:fixture@example.com:1082#third"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	code, message, result := errorResult(t, w)
	if code != "import_commit_failed" || message != resultMessage("import_commit_failed", nil) {
		t.Fatalf("error body: %s", w.Body.String())
	}
	if result.Outcome != "failed" || result.Stage != stageCommit || result.Counts.Accepted != 0 || result.Counts.Total != nil {
		t.Fatalf("commit result: %+v", result)
	}
	if result.Counts.Cached != nil || result.Counts.Usable != nil {
		t.Fatalf("failed new source claimed an existing cache: %+v", result.Counts)
	}
	if result.LastSuccessAt != nil {
		t.Fatalf("failed new source claimed a previous success: %+v", result)
	}
	for _, leaked := range []string{"fixture abort", "HTTP 0", "INSERT INTO", "no such"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Fatalf("response leaked %q: %s", leaked, w.Body.String())
		}
	}
	// The source keeps its single endpoint, its name and its recorded success.
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 || items[0].Name != "paste" || items[0].Status != "ok" || items[0].LastSuccess != first.AttemptedAt {
		t.Fatalf("failed commit changed the source: %+v %v", items, err)
	}
	assertCache(t, s, key, 1, "first")
	if after := identitySnapshot(t, s, key, true); after != before {
		t.Fatal("failed new paste changed the existing source or nodes")
	}
	var count int
	if err = s.Database.QueryRow("SELECT count(*) FROM imported_proxies").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed new paste left partial nodes: %d %v", count, err)
	}
}
