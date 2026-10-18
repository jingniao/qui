package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/restoretasks"
	"testing"
)

func TestRestoreProgressOwnerOnlyPaginationAndTamperRefusal(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	key, err := os.ReadFile(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	store := restoretasks.Store{Directory: filepath.Join(filepath.Dir(app.Config.Database), "restore-progress"), Key: key}
	record, err := store.Begin(ctx, "snapshot_fixture_01", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if response := (budgetHTTP{app: app}).call("GET", "/api/v1/restore-records", nil); response.Code != 401 {
		t.Fatal("anonymous read progress", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "restore_reader", "password": "viewer-test-password", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "restore_reader", "password": "viewer-test-password"})
	var session auth.Session
	if json.Unmarshal(login.Body.Bytes(), &session) != nil || login.Code != 200 {
		t.Fatal(login.Code)
	}
	if response := (budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: session.CSRFToken}).call("GET", "/api/v1/restore-records", nil); response.Code != 403 {
		t.Fatal("Viewer read progress", response.Code)
	}
	var before, after int64
	app.Database.QueryRow("SELECT total_changes()").Scan(&before)
	response := owner.call("GET", "/api/v1/restore-records?page=1&pageSize=1", nil)
	var result struct {
		Items []restoretasks.Record
		Total int
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != record.ID {
		t.Fatal(response.Code, response.Body.String())
	}
	app.Database.QueryRow("SELECT total_changes()").Scan(&after)
	if before != after {
		t.Fatal("read-only query modified database")
	}
	if response.Header().Get("Cache-Control") != "no-store" || bytes.Contains(response.Body.Bytes(), key) || bytes.Contains(response.Body.Bytes(), []byte(`"mac"`)) {
		t.Fatal("progress exposed private material or allowed cache")
	}
	if response := owner.call("GET", "/api/v1/restore-records?page=2&pageSize=1", nil); response.Code != 200 {
		t.Fatal(response.Code)
	}
	if response := owner.call("GET", "/api/v1/restore-records?page=0", nil); response.Code != 400 {
		t.Fatal(response.Code)
	}
	path := filepath.Join(store.Directory, record.ID+".json")
	data, _ := os.ReadFile(path)
	data = bytes.Replace(data, []byte("preparing"), []byte("committed"), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if response := owner.call("GET", "/api/v1/restore-records", nil); response.Code != 409 {
		t.Fatal("unverified progress returned as valid", response.Code)
	}
}
