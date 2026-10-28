package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPortalFailedLoginCredentialsAndAudit(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "hardened_user")
	disabled := portalUser(t, app, owner, "disabled_login")
	if r := owner.call("PUT", "/api/v1/users/"+disabled.ID+"/login", map[string]any{"enabled": false}); r.Code != 204 {
		t.Fatal(r.Body.String())
	}
	client := budgetHTTP{app: app}
	var want string
	for i, input := range []struct{ name, password string }{
		{" unknown_user ", portalPassword}, {disabled.Username, portalPassword},
		{u.Username, "incorrect"}, {u.Username, strings.Repeat("x", 513)},
	} {
		r := client.call("POST", "/api/v1/portal/auth/login", map[string]string{"username": input.name, "password": input.password})
		if r.Code != 401 {
			t.Fatalf("case %d: %d %s", i, r.Code, r.Body.String())
		}
		if i == 0 {
			want = r.Body.String()
		}
		if r.Body.String() != want {
			t.Fatalf("credentials response differs: %s", r.Body.String())
		}
		var count int
		if err := app.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE operator=? AND action='用户登录' AND result='失败' AND ip='192.0.2.1'", strings.TrimSpace(input.name)).Scan(&count); err != nil || count == 0 {
			t.Fatalf("missing failure audit: %v", err)
		}
	}
	var sessions int
	if err := app.Database.QueryRow("SELECT count(*) FROM user_sessions").Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("failed login issued session: %d %v", sessions, err)
	}
}

// Snapshot every table's rows, rather than only token counts: read-only previews
// must not touch last-login timestamps, audit rows, sessions or token material.
func portalDatabaseSnapshot(t *testing.T, app *Server) map[string][]string {
	t.Helper()
	rows, err := app.Database.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	result := map[string][]string{}
	for _, name := range names {
		r, err := app.Database.Query("SELECT * FROM \"" + strings.ReplaceAll(name, "\"", "\"\"") + "\"")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := r.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := r.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			result[name] = append(result[name], fmt.Sprintf("%#v", values))
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		r.Close()
	}
	return result
}

func TestPortalPreviewReadOnlyMatchesPortalAndOwnerOnly(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	u := portalUser(t, app, owner, "preview_user")
	client := portalLogin(t, app, u.Username, portalPassword)
	if r := owner.call("GET", "/api/v1/users/missing-user/portal-preview", nil); r.Code != 404 {
		t.Fatalf("unknown user preview: %d", r.Code)
	}
	if r := owner.call("GET", "/api/v1/users/"+u.ID, nil); !strings.Contains(r.Body.String(), "\"loginEnabled\":true") || strings.Contains(r.Body.String(), "passwordHash") {
		t.Fatal("login status missing or password hash exposed")
	}
	for _, state := range []string{"missing", "ready", "not_recoverable"} {
		if state == "ready" {
			if r := client.call("POST", "/api/v1/portal/subscription/reset", nil); r.Code != 201 {
				t.Fatal(r.Body.String())
			}
		}
		if state == "not_recoverable" {
			if _, err := app.Database.Exec("UPDATE subscription_tokens SET encrypted_token=NULL WHERE user_id=?", u.ID); err != nil {
				t.Fatal(err)
			}
		}
		before := portalDatabaseSnapshot(t, app)
		r := owner.call("GET", "/api/v1/users/"+u.ID+"/portal-preview", nil)
		if r.Code != 200 {
			t.Fatalf("preview: %d %s", r.Code, r.Body.String())
		}
		if len(r.Result().Cookies()) != 0 {
			t.Fatal("preview issued cookie")
		}
		var preview map[string]json.RawMessage
		if err := json.Unmarshal(r.Body.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{"me", "nodes", "subscription"} {
			live := client.call("GET", "/api/v1/portal/"+part, nil)
			var a, b any
			if live.Code != 200 {
				t.Fatal(live.Body.String())
			}
			if err := json.Unmarshal(preview[part], &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(live.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("%s differs: %s / %s", part, preview[part], live.Body.String())
			}
		}
		if !reflect.DeepEqual(before, portalDatabaseSnapshot(t, app)) {
			t.Fatal("preview changed database rows")
		}
	}
	if r := owner.call("POST", "/api/v1/admins", map[string]any{"username": "preview_viewer", "password": "viewer-preview-password", "displayName": "Viewer", "role": "Viewer"}); r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	viewer := budgetHTTP{app: app}
	r := viewer.call("POST", "/api/v1/auth/login", map[string]string{"username": "preview_viewer", "password": "viewer-preview-password"})
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &session) != nil {
		t.Fatal(r.Body.String())
	}
	viewer.cookie = r.Result().Cookies()[0]
	viewer.csrf = session.CSRFToken
	if r := viewer.call("GET", "/api/v1/users/"+u.ID+"/portal-preview", nil); r.Code != 403 {
		t.Fatalf("Viewer preview: %d", r.Code)
	}
}
