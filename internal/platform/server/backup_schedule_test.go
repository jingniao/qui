package server

import (
	"context"
	"encoding/json"
	"net"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"strings"
	"testing"
	"time"
)

func TestBackupScheduleOwnerCSRFViewerSecretIsolationAndRuns(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	path := "/api/v1/system/backup-schedule"
	policy := system.BackupPolicy{Enabled: true, Time: "03:00", DailyKeep: 7, WeeklyKeep: 4, MonthlyKeep: 3}
	password := "scheduled-http-backup-password"
	input := system.BackupScheduleInput{Policy: policy, Password: password}
	if response := (budgetHTTP{app: app}).call("GET", path, nil); response.Code != 401 {
		t.Fatal("anonymous schedule read", response.Code)
	}
	if response := (budgetHTTP{app: app, cookie: owner.cookie}).call("PUT", path, input); response.Code != 403 {
		t.Fatal("schedule CSRF bypass", response.Code)
	}
	response := owner.call("PUT", path, input)
	var saved system.BackupSchedule
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &saved) != nil || saved.Revision != 1 || !saved.PasswordConfigured {
		t.Fatal(response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), password) || strings.Contains(response.Body.String(), "encrypted_password") {
		t.Fatal("schedule secret exposed")
	}
	if response := owner.call("PUT", path, map[string]any{"policy": policy, "expectedRevision": 1, "password": password, "command": "shell"}); response.Code != 400 {
		t.Fatal("unknown operation accepted", response.Code)
	}
	policy.Time = "04:00"
	if response := owner.call("PUT", path, system.BackupScheduleInput{Policy: policy}); response.Code != 409 {
		t.Fatal("stale policy update accepted", response.Code)
	}
	if response := owner.call("POST", "/api/v1/admins", map[string]any{"username": "schedule_viewer", "password": "schedule-viewer-password", "displayName": "Viewer", "role": "Viewer"}); response.Code != 201 {
		t.Fatal(response.Code)
	}
	login := (budgetHTTP{app: app}).call("POST", "/api/v1/auth/login", map[string]any{"username": "schedule_viewer", "password": "schedule-viewer-password"})
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	var session auth.Session
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	viewer := budgetHTTP{app: app, cookie: login.Result().Cookies()[0], csrf: session.CSRFToken}
	for _, request := range []struct{ method, path string }{{"GET", path}, {"PUT", path}, {"GET", "/api/v1/backup-runs"}} {
		if response := viewer.call(request.method, request.path, input); response.Code != 403 {
			t.Fatal("Viewer accessed backup settings/runs", request, response.Code)
		}
	}
	if err := app.backupService.RunDue(context.Background(), time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	response = owner.call("GET", "/api/v1/backup-runs?page=1&pageSize=1", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"total":3`) || strings.Contains(response.Body.String(), "claim_token") || strings.Contains(response.Body.String(), password) {
		t.Fatal("runs missing pagination or exposed private data", response.Code, response.Body.String())
	}
}

func TestRealServerBackupWorkerWakesOnSettingsSaveAndStopsWithServer(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		httpListener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, httpListener, grpcListener) }()
	policy := system.BackupPolicy{Enabled: true, Time: "00:00", DailyKeep: 7, WeeklyKeep: 4, MonthlyKeep: 3}
	response := owner.call("PUT", "/api/v1/system/backup-schedule", system.BackupScheduleInput{Policy: policy, Password: "real-worker-backup-password"})
	if response.Code != 200 {
		cancel()
		<-done
		t.Fatal(response.Code, response.Body.String())
	}
	deadline := time.Now().Add(60 * time.Second)
	total := 0
	for time.Now().Before(deadline) {
		var err error
		_, total, err = app.backupService.Repository.Backups(context.Background(), 1, 20)
		if err == nil && total == 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if total != 3 {
		cancel()
		<-done
		t.Fatal("actual worker did not create daily/week/month encrypted files", total)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("backup worker prevented shutdown")
	}
}
