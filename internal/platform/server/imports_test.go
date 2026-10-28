package server

import (
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/importer"
)

func TestImportAPIOwnerOnlyEncryptedAndSubscriptionReadsCache(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "import_http")
	created := owner.call("POST", "/api/v1/subscription-sources", map[string]any{"name": "HTTP fixture", "content": "socks5://fixture:fixture@example.com:1080#external", "intervalSeconds": 60, "userIds": []string{u.ID}})
	var saved struct {
		ID string `json:"id"`
	}
	if created.Code != 200 || json.Unmarshal(created.Body.Bytes(), &saved) != nil {
		t.Fatalf("create source: %d", created.Code)
	}
	list := owner.call("GET", "/api/v1/subscription-sources", nil)
	if list.Code != 200 || strings.Contains(list.Body.String(), "example.com") {
		t.Fatal("source URL exposed")
	}
	malformed := owner.call("POST", "/api/v1/subscription-sources", map[string]any{"unexpected": true})
	if malformed.Code != 400 {
		t.Fatal("invalid request accepted")
	}
	// An unreachable URL is saved without fetching. Reading the subscription
	// uses the cache and produces a warning, never a network request or revision.
	if _, err := app.importStore.Save(t.Context(), "", importer.Input{Name: "pending fixture", URL: "https://127.0.0.1/fixture", Interval: 60, UserIDs: []string{u.ID}}); err != nil {
		t.Fatal(err)
	}
	repo := &subscription.Repository{Database: app.Database}
	var identity string
	if err := app.Database.QueryRow("SELECT id FROM devices WHERE user_id=?", u.ID).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	_, token, err := repo.Rotate(t.Context(), u.ID, identity)
	if err != nil {
		t.Fatal(err)
	}
	response := owner.call("GET", "/sub/"+token+"?format=clash", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "example.com") || strings.Contains(response.Body.String(), "pending fixture") {
		t.Fatal("cached subscription failed")
	}
	if r := owner.call("POST", "/api/v1/admins", map[string]any{"username": "import_viewer", "password": "import-viewer-password", "displayName": "Viewer", "role": "Viewer"}); r.Code != 201 {
		t.Fatal("create viewer")
	}
	viewer := budgetHTTP{app: app}
	login := viewer.call("POST", "/api/v1/auth/login", map[string]string{"username": "import_viewer", "password": "import-viewer-password"})
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if login.Code != 200 || json.Unmarshal(login.Body.Bytes(), &session) != nil {
		t.Fatal("viewer login")
	}
	viewer.cookie = login.Result().Cookies()[0]
	viewer.csrf = session.CSRFToken
	for _, path := range []string{"/api/v1/subscription-sources", "/api/v1/imported-proxies"} {
		if viewer.call("GET", path, nil).Code != 403 {
			t.Fatal("Viewer accessed import administration")
		}
	}
	for _, table := range []string{"config_revisions", "outbounds"} {
		var count int
		if err := app.Database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("import reached %s", table)
		}
	}
	c := portalLogin(t, app, u.Username, portalPassword)
	for _, path := range []string{"/api/v1/subscription-sources", "/api/v1/imported-proxies"} {
		if r := c.call("GET", path, nil); r.Code != 401 && r.Code != 403 {
			t.Fatal("portal accessed import administration")
		}
	}
}
