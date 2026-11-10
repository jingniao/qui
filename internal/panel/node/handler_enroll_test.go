package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnrollmentStoresConnectHost(t *testing.T) {
	p := newReissuePanel(t)
	cookie, csrf := p.session(t, "Owner")

	body := `{"name":"NodeWithConnectHost","connectHost":"node.example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/enrollment-token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	p.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Node Node `json:"node"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Node.ConnectHost != "node.example.com" {
		t.Fatalf("expected connectHost 'node.example.com', got %q", resp.Node.ConnectHost)
	}

	stored, err := p.repository.Get(context.Background(), resp.Node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectHost != "node.example.com" {
		t.Fatalf("stored connectHost expected 'node.example.com', got %q", stored.ConnectHost)
	}
}

func TestEnrollmentRejectsBadConnectHost(t *testing.T) {
	p := newReissuePanel(t)
	cookie, csrf := p.session(t, "Owner")

	for _, bad := range []string{"https://foo.com:8443", "host with spaces", "bad:host:port"} {
		body := `{"name":"BadHostNode","connectHost":"` + bad + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/enrollment-token", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(cookie)

		w := httptest.NewRecorder()
		p.mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bad connect host %q, got %d: %s", bad, w.Code, w.Body.String())
		}
	}
}
