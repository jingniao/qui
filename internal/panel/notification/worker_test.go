package notification

import (
	"context"
	"testing"
)

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
		{alert: "auto_quota_failed", wantTitle: "自动额度发布失败"},
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
