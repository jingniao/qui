package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"strings"
	"testing"
)

func importMultipart(t *testing.T, data []byte, password, fault string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writePassword := func() {
		if err := writer.WriteField("password", password); err != nil {
			t.Fatal(err)
		}
	}
	writeFile := func() {
		part, err := writer.CreateFormFile("archive", "../../outside.pcb")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if fault == "file-first" {
		writeFile()
		writePassword()
	} else {
		writePassword()
		if fault != "missing-file" {
			writeFile()
		}
	}
	if fault == "extra" {
		if err := writer.WriteField("path", "outside.db"); err != nil {
			t.Fatal(err)
		}
	}
	if fault == "duplicate" {
		writeFile()
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func TestBackupImportUploadPermissionsValidationAndActualPortableDownload(t *testing.T) {
	source := testServer(t)
	sourceOwner := budgetOwner(t, source)
	password := "portable-http-import-password"
	created := sourceOwner.call("POST", "/api/v1/backups", map[string]any{"password": password})
	var original system.Backup
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &original) != nil {
		t.Fatal(created.Code, created.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source.Config.Database), "backups", original.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	target := testServer(t)
	owner := budgetOwner(t, target)
	call := func(body []byte, contentType string, cookie *http.Cookie, csrf string, query string, length int64) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/api/v1/backups/import"+query, bytes.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if length >= 0 {
			request.ContentLength = length
		}
		response := httptest.NewRecorder()
		target.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	body, contentType := importMultipart(t, data, password, "")
	if response := call(body, contentType, nil, "", "", -1); response.Code != 401 {
		t.Fatal("anonymous uploaded backup", response.Code)
	}
	if response := call(body, contentType, owner.cookie, "", "", -1); response.Code != 403 {
		t.Fatal("import CSRF bypass", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "import_viewer", "password": "import-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: target}).call("POST", "/api/v1/auth/login", map[string]any{"username": "import_viewer", "password": "import-viewer-password"})
	var viewer auth.Session
	if login.Code != 200 || json.Unmarshal(login.Body.Bytes(), &viewer) != nil {
		t.Fatal(login.Code)
	}
	if response := call(body, contentType, login.Result().Cookies()[0], viewer.CSRFToken, "", -1); response.Code != 403 {
		t.Fatal("Viewer uploaded backup", response.Code)
	}
	for _, fault := range []string{"file-first", "missing-file", "extra", "duplicate"} {
		bad, media := importMultipart(t, data, password, fault)
		if response := call(bad, media, owner.cookie, owner.csrf, "", -1); response.Code != 400 {
			t.Fatal("invalid multipart accepted", fault, response.Code, response.Body.String())
		}
	}
	if response := call(body, "application/json", owner.cookie, owner.csrf, "", -1); response.Code != 400 {
		t.Fatal("JSON upload accepted", response.Code)
	}
	if response := call(body, contentType, owner.cookie, owner.csrf, "?password=secret", -1); response.Code != 400 {
		t.Fatal("query upload accepted", response.Code)
	}
	if response := call(body, contentType, owner.cookie, owner.csrf, "", archive.MaxArchiveBytes+(64<<10)+1); response.Code != 400 {
		t.Fatal("oversized request header accepted", response.Code)
	}
	wrong, media := importMultipart(t, data, "different-import-password", "")
	if response := call(wrong, media, owner.cookie, owner.csrf, "", -1); response.Code != 400 {
		t.Fatal("wrong password imported", response.Code)
	}
	if _, total, err := target.backupService.Repository.Backups(context.Background(), 1, 20); err != nil || total != 0 {
		t.Fatal("failed uploads left registered backup", total, err)
	}
	// Upload through actual HTTP transport, with malicious filename ignored.
	server := httptest.NewServer(target.HTTP.Handler)
	defer server.Close()
	request, err := http.NewRequest("POST", server.URL+"/api/v1/backups/import", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-CSRF-Token", owner.csrf)
	request.AddCookie(owner.cookie)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var imported system.Backup
	err = json.NewDecoder(response.Body).Decode(&imported)
	response.Body.Close()
	if err != nil || response.StatusCode != 201 || imported.ID != original.ID || imported.Origin != "imported" {
		t.Fatal("actual portable upload failed", response.StatusCode, imported, err)
	}
	if response := call(body, contentType, owner.cookie, owner.csrf, "", -1); response.Code != 200 {
		t.Fatal("repeat upload not idempotent", response.Code)
	}
	download := owner.call("GET", "/api/v1/backups/"+imported.ID+"/download", nil)
	if download.Code != 200 || !bytes.Equal(download.Body.Bytes(), data) {
		t.Fatal("import did not preserve original encrypted bytes", download.Code)
	}
	if response := owner.call("POST", "/api/v1/backups/"+imported.ID+"/verify", map[string]any{"password": password}); response.Code != 200 {
		t.Fatal("imported foreign materials did not verify", response.Code)
	}
	var secrets int
	if err := target.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE target LIKE ?", "%"+password+"%").Scan(&secrets); err != nil || secrets != 0 {
		t.Fatal("upload password entered audit", err)
	}
	listed := owner.call("GET", "/api/v1/backups", nil)
	if strings.Contains(listed.Body.String(), password) || strings.Contains(listed.Body.String(), target.Config.NodeIdentityDirectory) {
		t.Fatal("import catalog disclosed password or paths")
	}
}
