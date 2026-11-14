package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
)

func TestPortalImportedProxyDeletionGuardsAndLastSource(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	src, _, err := app.importStore.Save(context.Background(), "", importer.Input{Name: "single", Content: "socks5://user:pass@example.com:1080#Original", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err = app.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&key); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/imported-proxies/" + key
	r := owner.call("GET", path+"/deletion-preview", nil)
	var preview importer.DeletionPreview
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &preview) != nil || preview.Node.ID != key || preview.Node.Name != "Original" || preview.Node.Protocol != "socks5" || !preview.LastInSource || len(preview.Groups) != 0 || len(preview.ClientPolicies) != 0 || len(preview.Canvases) != 0 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	r = owner.call("DELETE", path, importer.DeleteInput{ExpectedRevision: 2})
	if r.Code != 409 || !strings.Contains(r.Body.String(), `"code":"stale"`) {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	sealed, err := app.importStore.Vault.Seal([]byte("https://example.com/sub"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("UPDATE subscription_sources SET url_encrypted=? WHERE id=?", sealed, src); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "DELETE"} {
		target := path
		if method == "GET" {
			target += "/deletion-preview"
		}
		r = owner.call(method, target, importer.DeleteInput{ExpectedRevision: 1})
		if r.Code != 409 || !strings.Contains(r.Body.String(), `"code":"import_delete_unsupported"`) {
			t.Fatalf("%s %d %s", method, r.Code, r.Body.String())
		}
	}
	sealed, err = app.importStore.Vault.Seal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("UPDATE subscription_sources SET url_encrypted=? WHERE id=?", sealed, src); err != nil {
		t.Fatal(err)
	}
	withoutCSRF := owner
	withoutCSRF.csrf = ""
	if r = withoutCSRF.call("DELETE", path, importer.DeleteInput{ExpectedRevision: 1}); r.Code != 403 {
		t.Fatalf("csrf %d %s", r.Code, r.Body.String())
	}
	if r = owner.call("POST", "/api/v1/admins", map[string]any{"username": "delete_viewer", "password": "viewer-delete-password", "displayName": "Viewer", "role": "Viewer"}); r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	viewer := budgetHTTP{app: app}
	r = viewer.call("POST", "/api/v1/auth/login", map[string]string{"username": "delete_viewer", "password": "viewer-delete-password"})
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &session) != nil {
		t.Fatal(r.Body.String())
	}
	viewer.cookie = r.Result().Cookies()[0]
	viewer.csrf = session.CSRFToken
	if r = viewer.call("GET", path+"/deletion-preview", nil); r.Code != 403 {
		t.Fatalf("viewer preview %d", r.Code)
	}
	if r = viewer.call("DELETE", path, importer.DeleteInput{ExpectedRevision: 1}); r.Code != 403 {
		t.Fatalf("viewer delete %d", r.Code)
	}
	if r = owner.call("DELETE", path, importer.DeleteInput{ExpectedRevision: 1}); r.Code != 204 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	for _, table := range []string{"imported_proxies", "subscription_sources"} {
		var count int
		if err = app.Database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var count int
	if err = app.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='删除导入节点'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
}
