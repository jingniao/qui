package server

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"testing"
	"time"
)

func TestCycleSchedulerReactivatesThroughAutomaticRevision(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "cycle_user", "")
	f.publish(t)
	setQuota(t, f.ctx, f, account.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	flowWait(t, "quota removal", func() bool { return len(f.autoRevisions(t)) == 1 })
	f.apply(t, f.autoRevisions(t)[0].ID)
	f.idle(t)
	if _, err := f.app.Database.Exec("UPDATE users SET traffic_reset_cycle='monthly',traffic_reset_anchor='2024-01-31T00:00:00.000000000Z' WHERE id=?", account.ID); err != nil {
		t.Fatal(err)
	}
	scheduler := f.app.cycleScheduler
	scheduler.now = func() time.Time { return time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC) }
	if err := scheduler.step(f.ctx); err != nil {
		t.Fatal(err)
	}
	flowWait(t, "reset automatic revision", func() bool { return len(f.autoRevisions(t)) == 2 })
	revision := f.autoRevisions(t)[1]
	if !stateHasCredential(f.state(t, revision.ID), credential) {
		t.Fatal("cycle did not restore credential")
	}
	if err := scheduler.step(f.ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.autoRevisions(t)) != 2 {
		t.Fatal("duplicate cycle revision")
	}
}

func TestNodeMetadataPermissionsImportAndRenewalRestart(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "Renewal", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + n.ID + "/meta"
	meta := map[string]any{"region": "JP", "remark": "账单", "renewDate": "2026-10-31", "renewCycle": "/月", "renewPrice": "10", "renewCurrency": "USD", "renewNoticeDays": 7}
	if r := owner.call("PUT", path, meta); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	meta["region"] = "US"
	meta["importOnly"] = true
	if r := owner.call("PUT", path, meta); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	got, present, err := app.nodeRepository.Meta(ctx, n.ID)
	if err != nil || !present || got.Region != "JP" {
		t.Fatal(got, present, err)
	}
	// Changing this authenticated administrator's role exercises the same session
	// as Viewer: reads remain permitted, writes are rejected before mutation.
	if _, err = app.Database.Exec("UPDATE admins SET role='Viewer'"); err != nil {
		t.Fatal(err)
	}
	if r := owner.call("GET", path, nil); r.Code != 200 {
		t.Fatal("viewer read", r.Code, r.Body.String())
	}
	if r := owner.call("PUT", path, meta); r.Code != 403 {
		t.Fatal("viewer write", r.Code)
	}
	if _, err = app.Database.Exec("UPDATE admins SET role='Owner'"); err != nil {
		t.Fatal(err)
	}
	count := 0
	scheduler := app.cycleScheduler
	scheduler.publish = func(event string, payload any) {
		if event == "system.alert" {
			count++
		}
	}
	scheduler.now = func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }
	if err = scheduler.step(ctx); err != nil {
		t.Fatal(err)
	}
	restart := *scheduler
	restart.nodes = &node.Repository{Database: app.Database}
	if err = restart.step(ctx); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("duplicate reminder", count)
	}
	restart.now = func() time.Time { return time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC) }
	if err = restart.step(ctx); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("next day reminder", count)
	}
	r := owner.call("POST", "/api/v1/users", user.Input{Username: "reset_api", Status: "active", AllowedProtocols: []string{"socks5"}, TrafficResetCycle: "30days", TrafficResetAnchor: "2024-01-31T00:00:00Z"})
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var u user.User
	if err = json.Unmarshal(r.Body.Bytes(), &u); err != nil || u.TrafficResetCycle != "30days" {
		t.Fatal(u, err)
	}
}

func TestCycleSchedulerCancelledRunJoinsBeforeReturn(t *testing.T) {
	app := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.cycleScheduler.now = func() time.Time { t.Error("cancelled scheduler accessed clock"); return time.Now() }
	done := make(chan struct{})
	go func() { app.cycleScheduler.run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not join")
	}
}
