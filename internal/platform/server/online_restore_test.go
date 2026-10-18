package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/restoretasks"
	"testing"
)

func TestOnlineRestoreAuthenticationConfirmationAndDispatchBoundary(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	password := "online-security-password"
	created := owner.call("POST", "/api/v1/backups", map[string]any{"password": password})
	var backup system.Backup
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &backup) != nil {
		t.Fatal(created.Code)
	}
	path := "/api/v1/backups/" + backup.ID + "/restore"
	body := map[string]any{"password": password, "archiveSha256": backup.SHA256, "confirmBackupId": backup.ID}
	if response := owner.call("POST", path, body); response.Code != 503 {
		t.Fatal("unmanaged server accepted restore", response.Code)
	}
	reserved, accepted, dispatched := 0, 0, 0
	app.ConfigureRestore(&system.OnlineRestore{Target: archive.RestoreTarget{Panel: app.Config, ConfigurationPath: filepath.Join(t.TempDir(), "panel.yaml")}, Reserve: func() (func(), error) { reserved++; return func() {}, nil }, Accept: func(ctx context.Context, prepared archive.PreparedRestore, secret []byte, actor, ip string) (restoretasks.Record, func(), error) {
		accepted++
		if string(secret) != password || actor == "" {
			t.Fatal("accepted request identity changed")
		}
		clear(secret)
		archive.DiscardPreparedRestore(ctx, prepared)
		return restoretasks.Record{ID: "accepted_record_01"}, func() { dispatched++ }, nil
	}})
	if response := (budgetHTTP{app: app}).call("POST", path, body); response.Code != 401 {
		t.Fatal("anonymous restore accepted", response.Code)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("POST", path, body); response.Code != 403 {
		t.Fatal("restore bypassed CSRF", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "online_viewer", "password": "viewer-test-password", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "online_viewer", "password": "viewer-test-password"})
	var session auth.Session
	json.Unmarshal(login.Body.Bytes(), &session)
	if response := (budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: session.CSRFToken}).call("POST", path, body); response.Code != 403 {
		t.Fatal("Viewer restored system", response.Code)
	}
	if reserved != 0 {
		t.Fatal("unauthorized requests entered restore preparation")
	}
	if response := owner.call("POST", path, map[string]any{"password": password, "archiveSha256": backup.SHA256, "confirmBackupId": backup.ID, "command": "unexpected"}); response.Code != 400 {
		t.Fatal("undefined operation accepted", response.Code)
	}
	if response := owner.call("POST", path, map[string]any{"password": "incorrect-password", "archiveSha256": backup.SHA256, "confirmBackupId": backup.ID}); response.Code != 400 {
		t.Fatal("wrong password accepted", response.Code)
	}
	if accepted != 0 || dispatched != 0 {
		t.Fatal("failed verification dispatched restore")
	}
	response := owner.call("POST", path, body)
	if response.Code != 202 || accepted != 1 || dispatched != 1 {
		t.Fatal("verified request was not dispatched once", response.Code, accepted, dispatched)
	}
}
