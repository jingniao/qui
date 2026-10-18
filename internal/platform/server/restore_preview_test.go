package server

import (
	"encoding/json"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"strings"
	"testing"
)

func TestRestorePreviewOwnerCSRFViewerAndVerifiedReadOnlyInventory(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	password := "restore-preview-http-password"
	created := owner.call("POST", "/api/v1/backups", map[string]any{"password": password})
	var item system.Backup
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &item) != nil {
		t.Fatal(created.Code, created.Body.String())
	}
	path := "/api/v1/backups/" + item.ID + "/restore-preview"
	body := map[string]any{"password": password}
	if response := (budgetHTTP{app: app}).call("POST", path, body); response.Code != 401 {
		t.Fatal("anonymous read restore inventory", response.Code)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("POST", path, body); response.Code != 403 {
		t.Fatal("restore preview CSRF bypass", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "restore_viewer", "password": "restore-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "restore_viewer", "password": "restore-viewer-password"})
	var viewer auth.Session
	if login.Code != 200 || json.Unmarshal(login.Body.Bytes(), &viewer) != nil {
		t.Fatal(login.Code)
	}
	if response := (budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: viewer.CSRFToken}).call("POST", path, body); response.Code != 403 {
		t.Fatal("Viewer read restore inventory", response.Code)
	}
	if response := owner.call("POST", path, map[string]any{"password": password, "execute": true}); response.Code != 400 {
		t.Fatal("preview accepted execute option", response.Code)
	}
	if response := owner.call("POST", path, map[string]any{"password": "wrong-restore-password"}); response.Code != 400 {
		t.Fatal("wrong password read snapshot", response.Code)
	}
	var before, after int64
	app.Database.QueryRow("SELECT total_changes()").Scan(&before)
	response := owner.call("POST", path, body)
	var result system.RestorePreview
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatal(response.Code, response.Body.String())
	}
	app.Database.QueryRow("SELECT total_changes()").Scan(&after)
	if before != after {
		t.Fatal("restore preview wrote database state")
	}
	if result.Snapshot.Admins != 1 || result.Current.Admins != 2 || result.Snapshot.ActiveOwners != 1 || result.BackupID != item.ID || result.ArchiveSHA256 != item.SHA256 || result.ExecutionAvailable {
		t.Fatal("incorrect snapshot/current preview", result)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("restore inventory allowed cache")
	}
	for _, marker := range []string{password, "password_hash", "configuration.key", "KeyDigest", "NodeCADigest", "BEGIN PRIVATE KEY"} {
		if strings.Contains(response.Body.String(), marker) {
			t.Fatal("confidential restore contents returned", marker)
		}
	}
}
