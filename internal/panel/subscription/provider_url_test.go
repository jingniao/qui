package subscription

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// v1.5.4 item 12: behind a TLS-terminating reverse proxy the request arrives
// over plain HTTP; the saved panel address keeps rule links on https and the
// public name.
func TestProviderURLPrefersSavedPanelAddress(t *testing.T) {
	t.Parallel()
	plain := httptest.NewRequest("GET", "http://127.0.0.1:22550/sub/t", nil)
	secure := httptest.NewRequest("GET", "https://panel.example:22550/sub/t", nil)
	secure.TLS = &tls.ConnectionState{}
	saved := func(host string, port int) func(context.Context) (string, int) {
		return func(context.Context) (string, int) { return host, port }
	}
	for _, test := range []struct {
		name    string
		request *http.Request
		panel   func(context.Context) (string, int)
		want    string
	}{
		{"unset keeps the request", plain, saved("", 0), "http://127.0.0.1:22550/sub/tok/rules/svc"},
		{"no setting source", secure, nil, "https://panel.example:22550/sub/tok/rules/svc"},
		{"saved host and port", plain, saved("qui.example.com", 443), "https://qui.example.com/sub/tok/rules/svc"},
		{"saved host keeps request port", plain, saved("qui.example.com", 0), "https://qui.example.com:22550/sub/tok/rules/svc"},
		{"saved port only", plain, saved("", 8443), "https://127.0.0.1:8443/sub/tok/rules/svc"},
	} {
		if got := providerURLFor(test.request, "tok", test.panel)("svc"); got != test.want {
			t.Errorf("%s: %s, want %s", test.name, got, test.want)
		}
	}
}
