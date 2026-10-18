package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"strings"
	"testing"
)

func TestBackupsOwnerCSRFViewerIsolationEncryptedDownloadAndActualOperations(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	password := "portable-http-backup-password"
	body := map[string]any{"password": password}
	if response := (budgetHTTP{app: app}).call("POST", "/api/v1/backups", body); response.Code != 401 {
		t.Fatal("anonymous backup created", response.Code)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("POST", "/api/v1/backups", body); response.Code != 403 {
		t.Fatal("backup CSRF bypass", response.Code)
	}
	if response := owner.call("POST", "/api/v1/backups", map[string]any{"password": password, "path": "outside.db"}); response.Code != 400 {
		t.Fatal("arbitrary backup file request accepted", response.Code)
	}
	if response := owner.call("POST", "/api/v1/backups", map[string]any{"password": "short"}); response.Code != 400 {
		t.Fatal("short backup password accepted", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "backup_viewer", "password": "backup-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "backup_viewer", "password": "backup-viewer-password"})
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	var session auth.Session
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	viewer := budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: session.CSRFToken}
	for _, request := range []struct{ method, path string }{{"GET", "/api/v1/backups"}, {"POST", "/api/v1/backups"}, {"GET", "/api/v1/backups/unknown/download"}, {"POST", "/api/v1/backups/unknown/verify"}, {"DELETE", "/api/v1/backups/unknown"}} {
		if response := viewer.call(request.method, request.path, body); response.Code != 403 {
			t.Fatal("Viewer accessed backups", request, response.Code)
		}
	}
	response := owner.call("POST", "/api/v1/backups", body)
	var item system.Backup
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &item) != nil || item.ID == "" || item.FileBytes == 0 {
		t.Fatal("actual backup creation failed", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), password) || strings.Contains(response.Body.String(), app.Config.NodeIdentityDirectory) {
		t.Fatal("backup response disclosed password or paths")
	}
	response = owner.call("GET", "/api/v1/backups?page=1&pageSize=1", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"total":1`) || !strings.Contains(response.Body.String(), item.ID) {
		t.Fatal("catalog not served with server pagination", response.Code)
	}
	path := "/api/v1/backups/" + item.ID
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("POST", path+"/verify", body); response.Code != 403 {
		t.Fatal("verification CSRF bypass", response.Code)
	}
	if response := owner.call("POST", path+"/verify", map[string]any{"password": "incorrect-backup-password"}); response.Code != 400 {
		t.Fatal("wrong password verified", response.Code)
	}
	if response := owner.call("POST", path+"/verify", body); response.Code != 200 {
		t.Fatal("real backup verification failed", response.Code, response.Body.String())
	}
	server := httptest.NewServer(app.HTTP.Handler)
	defer server.Close()
	request, err := http.NewRequest("GET", server.URL+path+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(owner.cookie)
	result, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil || result.StatusCode != 200 || int64(len(data)) != item.FileBytes || !bytes.HasPrefix(data, []byte("PCBKP001")) {
		t.Fatal("download did not stream real encrypted file", result.StatusCode, err)
	}
	if result.Header.Get("Cache-Control") != "no-store" || !strings.Contains(result.Header.Get("Content-Disposition"), ".pcb") {
		t.Fatal("download headers missing")
	}
	for _, marker := range [][]byte{[]byte(password), []byte("BEGIN PRIVATE KEY"), []byte("SQLite format 3")} {
		if bytes.Contains(data, marker) {
			t.Fatal("download exposed plaintext contents")
		}
	}
	request, err = http.NewRequest("GET", server.URL+path+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(owner.cookie)
	request.Header.Set("Range", "bytes=0-39")
	result, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil || result.StatusCode != 206 || len(partial) != 40 {
		t.Fatal("encrypted range download unavailable", result.StatusCode, len(partial), err)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("DELETE", path, nil); response.Code != 403 {
		t.Fatal("delete CSRF bypass", response.Code)
	}
	if response := owner.call("DELETE", path, nil); response.Code != 204 {
		t.Fatal("real delete failed", response.Code)
	}
	if response := owner.call("GET", path+"/download", nil); response.Code != 404 {
		t.Fatal("deleted archive still downloadable", response.Code)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(app.Config.Database), "backups", item.ID+".pcb")); !os.IsNotExist(err) {
		t.Fatal("delete left backup file", err)
	}
	var secrets int
	if err := app.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE target LIKE ?", "%"+password+"%").Scan(&secrets); err != nil || secrets != 0 {
		t.Fatal("backup password entered audit", err)
	}
}
