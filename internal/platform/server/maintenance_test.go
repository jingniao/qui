package server

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"strings"
	"testing"
	"time"
)

func TestAutoRenewOmissionAndSchedulerOrder(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "auto renewal", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + n.ID + "/meta"
	meta := map[string]any{"renewDate": "2025-01-31", "renewCycle": "/月", "autoRenew": true, "renewNoticeDays": 0}
	if res := owner.call("PUT", path, meta); res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	delete(meta, "autoRenew")
	if res := owner.call("PUT", path, meta); res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	stored, _, err := app.nodeRepository.Meta(ctx, n.ID)
	if err != nil || !stored.AutoRenew {
		t.Fatal(stored, err)
	}
	meta["renewCycle"] = "一次性"
	if res := owner.call("PUT", path, meta); res.Code != 400 {
		t.Fatal(res.Code, res.Body.String())
	}
	stored, _, err = app.nodeRepository.Meta(ctx, n.ID)
	if err != nil || !stored.AutoRenew || stored.RenewCycle != "/月" {
		t.Fatal("rejected write changed metadata", stored, err)
	}
	scheduler := app.cycleScheduler
	count := 0
	scheduler.publish = func(event string, payload any) {
		if event != "system.alert" {
			t.Fatalf("unexpected event %s", event)
		}
		data := payload.(map[string]string)
		if data["alert"] != "node_renewal" || data["nodeId"] != n.ID {
			t.Fatal(payload)
		}
		if !strings.Contains(data["message"], "服务器「auto renewal」") || !strings.Contains(data["message"], "2025-01-31") || !strings.Contains(data["message"], "已开启自动续费") {
			t.Fatal("missing renewal body", data)
		}
		m, _, err := app.nodeRepository.Meta(ctx, n.ID)
		if err != nil || m.RenewDate != "2025-01-31" {
			t.Fatal("date advanced before reminder", m, err)
		}
		count++
	}
	scheduler.now = func() time.Time { return time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC) }
	if err := scheduler.step(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _, err = app.nodeRepository.Meta(ctx, n.ID)
	if err != nil || stored.RenewDate != "2025-01-31" || count != 1 {
		t.Fatal(stored, count, err)
	}
	scheduler.now = func() time.Time { return time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC) }
	if err := scheduler.step(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _, err = app.nodeRepository.Meta(ctx, n.ID)
	if err != nil || stored.RenewDate != "2025-02-28" || count != 2 {
		t.Fatal(stored, count, err)
	}
	var audits int
	if err := app.Database.QueryRow("SELECT COUNT(*) FROM admin_operations WHERE operator='system' AND action=? AND target=?", "自动续费 2025-01-31 → 2025-02-28", n.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
}

func TestRenewalAlertCalendarDetails(t *testing.T) {
	for _, tc := range []struct{ date, part string }{{"2026-03-09", "还剩 2 天"}, {"2026-03-07", "今天（2026-03-07）到期"}, {"2026-03-06", "已过 1 天"}, {"1600-03-07", "已过 155593 天"}} {
		t.Run(tc.date, func(t *testing.T) {
			app := testServer(t)
			owner := budgetOwner(t, app)
			ctx := context.Background()
			n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "renewal server", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if res := owner.call("PUT", "/api/v1/nodes/"+n.ID+"/meta", map[string]any{"renewDate": tc.date, "renewNoticeDays": 7, "renewCycle": "一次性"}); res.Code != 200 {
				t.Fatal(res.Body.String())
			}
			app.cycleScheduler.now = func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) }
			var message string
			app.cycleScheduler.publish = func(_ string, payload any) { message = payload.(map[string]string)["message"] }
			if err = app.cycleScheduler.step(ctx); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(message, tc.part) || !strings.Contains(message, "renewal server") {
				t.Fatal(message)
			}
		})
	}
	t.Run("DST uses calendar days", func(t *testing.T) {
		app := testServer(t)
		owner := budgetOwner(t, app)
		ctx := context.Background()
		n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "DST", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if res := owner.call("PUT", "/api/v1/nodes/"+n.ID+"/meta", map[string]any{"renewDate": "2026-03-09", "renewNoticeDays": 7, "renewCycle": "一次性"}); res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		location, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Fatal(err)
		}
		items, err := app.nodeRepository.RenewalAlerts(ctx, time.Date(2026, 3, 7, 23, 30, 0, 0, location), location)
		if err != nil || len(items) != 1 || items[0].DaysLeft != 2 {
			t.Fatal(items, err)
		}
	})
}

func TestCycleSchedulerReactivatesThroughAutomaticRevision(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
