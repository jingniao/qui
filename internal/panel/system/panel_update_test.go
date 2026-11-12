package system

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// releaseStub stands in for the GitHub "latest release" endpoint: it counts the
// requests so the cache and the refresh bypass are observable, and its answer can be
// swapped or failed between calls.
type releaseStub struct {
	mu       sync.Mutex
	requests int
	status   int
	body     string
}

func (s *releaseStub) set(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

func (s *releaseStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *releaseStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
			t.Errorf("release lookup headers: accept=%q agent=%q", r.Header.Get("Accept"), r.Header.Get("User-Agent"))
		}
		s.mu.Lock()
		s.requests++
		status, body := s.status, s.body
		s.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

const releaseBody150 = `{"tag_name":"v1.5.0","html_url":"https://github.com/jingniao/qui/releases/tag/v1.5.0","published_at":"2026-11-01T10:00:00Z","draft":false,"prerelease":false}`

func newPanelUpdateService(t *testing.T, stub *releaseStub, directory, current string) *PanelUpdateService {
	t.Helper()
	return &PanelUpdateService{Directory: directory, CurrentVersion: current, BaseURL: stub.server(t).URL, Client: &http.Client{}}
}

func TestPanelUpdateLookupIgnoresDraftsAndCaches(t *testing.T) {
	t.Parallel()
	stub := &releaseStub{}
	stub.set(0, releaseBody150)
	service := newPanelUpdateService(t, stub, t.TempDir(), "1.4.3")
	state := service.State(context.Background(), false)
	if state.Latest == nil || state.Latest.Version != "1.5.0" || state.Latest.URL != "https://github.com/jingniao/qui/releases/tag/v1.5.0" || state.Latest.PublishedAt != "2026-11-01T10:00:00Z" {
		t.Fatalf("latest release: %+v", state.Latest)
	}
	if state.LatestError != nil || !state.Supported || !state.UpdateAvailable {
		t.Fatalf("state: %+v", state)
	}
	// The successful answer is cached, so the second read asks nothing.
	if cached := service.State(context.Background(), false); cached.Latest == nil || stub.count() != 1 {
		t.Fatalf("cache miss: %d requests %+v", stub.count(), cached.Latest)
	}
	// The 检查更新 button bypasses the cache.
	if service.State(context.Background(), true).Latest == nil || stub.count() != 2 {
		t.Fatalf("refresh did not ask again: %d", stub.count())
	}
	// A release that is not newer than the running panel is not an update.
	same := newPanelUpdateService(t, stub, t.TempDir(), "1.5.0")
	if state := same.State(context.Background(), false); !state.Supported || state.UpdateAvailable {
		t.Fatalf("equal version offered an update: %+v", state)
	}
}

func TestPanelUpdateLookupRejectsUnusableReleases(t *testing.T) {
	t.Parallel()
	for name, answer := range map[string]struct {
		status int
		body   string
	}{
		"draft":            {0, `{"tag_name":"v1.5.0","html_url":"https://example.test/r","draft":true}`},
		"prerelease":       {0, `{"tag_name":"v1.5.0","html_url":"https://example.test/r","prerelease":true}`},
		"missing prefix":   {0, `{"tag_name":"1.5.0","html_url":"https://example.test/r"}`},
		"candidate tag":    {0, `{"tag_name":"v1.5.0-rc.1","html_url":"https://example.test/r"}`},
		"no release page":  {0, `{"tag_name":"v1.5.0"}`},
		"not json":         {0, `<html>rate limited</html>`},
		"upstream failure": {500, `{"tag_name":"v1.5.0","html_url":"https://example.test/r"}`},
	} {
		t.Run(name, func(t *testing.T) {
			stub := &releaseStub{}
			stub.set(answer.status, answer.body)
			service := newPanelUpdateService(t, stub, t.TempDir(), "1.4.3")
			state := service.State(context.Background(), false)
			if state.Latest != nil || state.LatestError == nil || *state.LatestError != "check_failed" || state.UpdateAvailable {
				t.Fatalf("unusable release answered %+v", state)
			}
			// A failure is never cached: the next read asks again.
			service.State(context.Background(), false)
			if stub.count() != 2 {
				t.Fatalf("failure was cached: %d requests", stub.count())
			}
		})
	}
}

func TestCompareReleaseVersions(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		left, right string
		want        int
	}{
		{"1.4.3", "1.4.3", 0},
		{"1.4.2", "1.4.3", -1},
		{"1.10.0", "1.9.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.4.3", "1.4.10", -1},
		// Versions that are not official compare equal, so they never offer an update.
		{"dev", "1.4.3", 0},
		{"1.5.0-rc.1", "1.4.3", 0},
	} {
		if got := compareReleaseVersions(item.left, item.right); got != item.want {
			t.Fatalf("compare %s %s = %d, want %d", item.left, item.right, got, item.want)
		}
	}
}

func TestPanelUpdateRequestIDIsRandomHex(t *testing.T) {
	t.Parallel()
	first, err := newPanelUpdateRequestID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPanelUpdateRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if !panelUpdateRequestIDPattern.MatchString(first) || first == second {
		t.Fatalf("request ids: %q %q", first, second)
	}
}
