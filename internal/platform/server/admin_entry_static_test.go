package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// v1.5.4 item 10: only the current admin login path gets the admin marker; the
// root, the old default and any other unknown path get the plain page, so the
// address is not disclosed.
func TestStaticHandlerMarksOnlyTheAdminEntry(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{"index.html": {Data: []byte("<html><head><title>x</title></head></html>")}}
	handler := staticHandler(files, func(context.Context) string { return "/manage-7f3k" })
	for path, marked := range map[string]bool{
		"/manage-7f3k":  true,
		"/manage-7f3k/": true,
		"/":             false,
		"/login":        false,
		"/dashboard":    false,
		"/manage-7f3":   false,
		"/random-path":  false,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		body := recorder.Body.String()
		if recorder.Code != 200 || !strings.Contains(body, "<title>x</title>") {
			t.Fatalf("%s: %d %q", path, recorder.Code, body)
		}
		if got := strings.Contains(body, adminEntryMarker); got != marked {
			t.Errorf("%s: marker present=%v, want %v", path, got, marked)
		}
		if strings.Contains(body, "manage-7f3k") && !marked {
			t.Errorf("%s discloses the admin path", path)
		}
	}
}
