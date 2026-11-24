package notification

import (
	"context"
	"testing"
	"time"
)

type startupSources struct{ nodes []NodeSnapshot }

func (s *startupSources) Nodes(context.Context) ([]NodeSnapshot, error) { return s.nodes, nil }
func (s *startupSources) Metrics(context.Context, time.Time) (map[string]MetricWindow, error) {
	return nil, nil
}
func (s *startupSources) Traffic(context.Context, time.Time) (map[string]TrafficSnapshot, error) {
	return nil, nil
}

func TestWorkerStartupOfflineObservation(t *testing.T) {
	for _, minutes := range []int{0, 3} {
		t.Run((time.Duration(minutes) * time.Minute).String(), func(t *testing.T) {
			repo, _ := newTestRepo(t)
			ctx := context.Background()
			if err := repo.SaveRules(ctx, []Rule{{Kind: "node_offline", Enabled: true, DurationMinutes: minutes}}); err != nil {
				t.Fatal(err)
			}
			source := &startupSources{nodes: []NodeSnapshot{{ID: "n", Status: "offline", LastHeartbeatRaw: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}}}
			worker := Worker{Repository: repo, Sources: source, startedAt: time.Now()}
			worker.tick(ctx)
			items, total, err := repo.List(ctx, false, false, 1, 20)
			if err != nil || total != 0 {
				t.Fatalf("startup notifications = %+v, %v", items, err)
			}
			// 0 minutes still needs a full heartbeat window after startup.
			worker.startedAt = time.Now().Add(-44 * time.Second)
			worker.tick(ctx)
			_, total, err = repo.List(ctx, false, false, 1, 20)
			if err != nil || total != 0 {
				t.Fatalf("alert before observation window: %d, %v", total, err)
			}
			worker.startedAt = time.Now().Add(-max(45*time.Second, time.Duration(minutes)*time.Minute) - time.Second)
			worker.tick(ctx)
			items, total, err = repo.List(ctx, false, false, 1, 20)
			if err != nil || total != 1 || items[0].Severity != "critical" {
				t.Fatalf("missing real offline: %+v, %v", items, err)
			}
			// Restarting again must keep the active episode without a duplicate.
			worker.startedAt = time.Now()
			worker.tick(ctx)
			source.nodes[0].Status = "online"
			source.nodes[0].LastHeartbeatRaw = time.Now().Format(time.RFC3339Nano)
			worker.tick(ctx)
			items, total, err = repo.List(ctx, false, false, 1, 20)
			if err != nil || total != 2 || items[0].Severity != "resolved" {
				t.Fatalf("recovery during observation: %+v, %v", items, err)
			}
		})
	}
}

func TestAlertMapsKnownKinds(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	worker := &Worker{
		Repository: repo,
	}

	for _, tc := range []struct {
		alert     string
		wantTitle string
	}{
		{alert: "certificate_renewal", wantTitle: "证书续期失败"},
		{alert: "certificate_deploy_failed", wantTitle: "证书自动发布失败"},
		{alert: "node_renewal", wantTitle: "服务器续费提醒"},
		{alert: "user_access_failed", wantTitle: "自动额度发布失败"},
		{alert: "other_unknown", wantTitle: "系统告警"},
	} {
		worker.Alert(ctx, map[string]any{
			"alert":   tc.alert,
			"message": "alert details",
		})
	}

	items, total, err := repo.List(ctx, false, false, 1, 10)
	if err != nil || total != 5 {
		t.Fatalf("expected 5 items, got total=%d, err=%v", total, err)
	}

	// Items are ordered by at DESC, id DESC
	expectedTitles := map[string]bool{
		"证书续期失败":   true,
		"证书自动发布失败": true,
		"服务器续费提醒":  true,
		"自动额度发布失败": true,
		"系统告警":     true,
	}
	for _, it := range items {
		if !expectedTitles[it.Title] {
			t.Fatalf("unexpected title %q", it.Title)
		}
		if it.Kind != "system_alert" {
			t.Fatalf("expected kind 'system_alert', got %q", it.Kind)
		}
	}

	// Disable system_alert rule: alerts must be ignored
	rules, err := repo.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rules {
		if rules[i].Kind == "system_alert" {
			rules[i].Enabled = false
		}
	}
	if err := repo.SaveRules(ctx, rules); err != nil {
		t.Fatal(err)
	}

	worker.Alert(ctx, map[string]any{"alert": "node_renewal", "message": "ignored"})
	itemsAfter, totalAfter, _ := repo.List(ctx, false, false, 1, 10)
	if totalAfter != 5 || len(itemsAfter) != 5 {
		t.Fatalf("alert should have been ignored when rule disabled, got total=%d", totalAfter)
	}
}

func TestAlertCarriesNodeAndBody(t *testing.T) {
	for _, anyMap := range []bool{false, true} {
		for _, tc := range []struct{ kind, title string }{{"node_renewal", "服务器续费提醒"}, {"user_access", "用户访问状态变化"}, {"device_limit", "设备上限触发"}} {
			repo, _ := newTestRepo(t)
			w := Worker{Repository: repo}
			var payload any = map[string]string{"alert": tc.kind, "nodeId": "deleted-node", "message": "服务器 details"}
			if anyMap {
				payload = map[string]any{"alert": tc.kind, "nodeId": "deleted-node", "message": "服务器 details"}
			}
			w.Alert(context.Background(), payload)
			items, n, err := repo.List(context.Background(), false, false, 1, 10)
			if err != nil || n != 1 || items[0].NodeID != "deleted-node" || items[0].Body != "服务器 details" || items[0].Title != tc.title {
				t.Fatal(items, n, err)
			}
		}
	}
}
