package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"strings"
	"testing"
	"time"
)

func TestBaseSettingsOwnerViewerCSRFAndSessionRefresh(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	path := "/api/v1/system/base"
	response := owner.call("GET", path, nil)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	var initial system.BaseSettings
	json.Unmarshal(response.Body.Bytes(), &initial)
	if initial.PlatformName != "Budget test" || initial.Timezone != "UTC" || initial.Revision != 0 {
		t.Fatal(initial)
	}
	input := system.BaseInput{PlatformName: "新的代理控制台", Timezone: "Asia/Tokyo"}
	if response := (budgetHTTP{app: app}).call("PUT", path, input); response.Code != 401 {
		t.Fatal("anonymous write accepted", response.Code)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("PUT", path, input); response.Code != 403 {
		t.Fatal("missing CSRF accepted", response.Code)
	}
	response = owner.call("PUT", path, input)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var saved system.BaseSettings
	json.Unmarshal(response.Body.Bytes(), &saved)
	if saved.PlatformName != input.PlatformName || saved.Timezone != input.Timezone || saved.Revision != 1 {
		t.Fatal(saved)
	}
	response = owner.call("GET", "/api/v1/auth/session", nil)
	var session auth.Session
	json.Unmarshal(response.Body.Bytes(), &session)
	if response.Code != 200 || !session.IsAuthenticated || session.PlatformName != input.PlatformName || session.Timezone != input.Timezone {
		t.Fatal("live session did not update shared display settings", response.Code)
	}
	if response := owner.call("PUT", path, map[string]any{"platformName": "Panel", "timezone": "UTC", "expectedRevision": 1, "role": "Owner"}); response.Code != 400 {
		t.Fatal("unknown base option accepted", response.Code)
	}
	if response := owner.call("PUT", path, system.BaseInput{PlatformName: "Panel", Timezone: "Local", ExpectedRevision: 1}); response.Code != 400 {
		t.Fatal("machine timezone accepted", response.Code)
	}
	if response := owner.call("PUT", path, system.BaseInput{PlatformName: "Panel", Timezone: "UTC"}); response.Code != 409 {
		t.Fatal("stale overwrite accepted", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "base_viewer", "password": "base-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	response = (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "base_viewer", "password": "base-viewer-password"})
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	json.Unmarshal(response.Body.Bytes(), &session)
	viewer := budgetHTTP{app: app, cookie: response.Result().Cookies()[0], csrf: session.CSRFToken}
	if response := viewer.call("GET", path, nil); response.Code != 403 {
		t.Fatal("Viewer read owner base settings", response.Code)
	}
	if response := viewer.call("PUT", path, input); response.Code != 403 {
		t.Fatal("Viewer wrote base settings", response.Code)
	}
	response = viewer.call("GET", "/api/v1/auth/session", nil)
	json.Unmarshal(response.Body.Bytes(), &session)
	if session.Timezone != input.Timezone {
		t.Fatal("Viewer display did not receive shared timezone")
	}
}
func TestBaseSettingsEventContainsOnlyRefreshHint(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	server := httptest.NewServer(app.HTTP.Handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(owner.cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if response := owner.call("PUT", "/api/v1/system/base", system.BaseInput{PlatformName: "PRIVATE-PLATFORM-NAME", Timezone: "Europe/London"}); response.Code != 200 {
		t.Fatal(response.Code)
	}
	event := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		event += line
		if strings.Contains(event, "event: system_settings") && strings.HasSuffix(event, "\n\n") {
			break
		}
	}
	if !strings.Contains(event, `data: {"changed":true}`) || strings.Contains(event, "PRIVATE-PLATFORM-NAME") || strings.Contains(event, "Europe/London") {
		t.Fatal("settings SSE contained private configuration instead of refresh hint")
	}
}
