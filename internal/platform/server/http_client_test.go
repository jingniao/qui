package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	"strings"
	"testing"
)

type budgetHTTP struct {
	app    *Server
	cookie *http.Cookie
	csrf   string
}

func (b budgetHTTP) call(method, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.1:1234"
	if b.cookie != nil {
		request.AddCookie(b.cookie)
	}
	request.Header.Set("X-CSRF-Token", b.csrf)
	response := httptest.NewRecorder()
	b.app.HTTP.Handler.ServeHTTP(response, request)
	return response
}

func budgetOwner(t *testing.T, app *Server) budgetHTTP {
	t.Helper()
	client := budgetHTTP{app: app}
	// The Setup username is historical; it has no budget meaning now.
	response := client.call("POST", "/api/v1/auth/setup", map[string]any{"token": app.SetupToken(), "username": "budget_owner", "password": "budget-owner-test-password", "platformName": "Budget test", "timezone": "UTC"})
	if response.Code != 201 {
		t.Fatal("owner initialization failed", response.Code)
	}
	var session auth.Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	client.cookie = response.Result().Cookies()[0]
	client.csrf = session.CSRFToken
	return client
}

func budgetRepositories(t *testing.T, app *Server) (*user.Repository, *node.Repository) {
	t.Helper()
	key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &user.Repository{Database: app.Database, Vault: key}, &node.Repository{Database: app.Database}
}
