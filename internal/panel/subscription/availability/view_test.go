package availability

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/testutil/dbtest"
)

func TestDisabledDependencyAndNotificationSwitch(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "availability.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := &node.Repository{Database: database}
	n, _, _, err := repo.CreateEnrollment(ctx, "dependency", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := database.Exec(`UPDATE notification_rules SET enabled=0,duration_minutes=3 WHERE kind='node_offline'`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		enabled, maintenance bool
		age                  time.Duration
		reason               string
	}{
		{true, false, time.Minute, ""},
		{true, false, 4 * time.Minute, "确认离线"},
		{false, false, time.Second, "已禁用"},
		{true, true, time.Second, "维护中"},
	} {
		if _, err := database.Exec(`UPDATE nodes SET status='online',enabled=?,maintenance=?,last_heartbeat=? WHERE id=?`, tc.enabled, tc.maintenance, now.Add(-tc.age).Format(time.RFC3339Nano), n.ID); err != nil {
			t.Fatal(err)
		}
		view, err := readView(ctx, database, now)
		if err != nil {
			t.Fatal(err)
		}
		if view.OfflineAfterSeconds != 180 {
			t.Fatalf("duration = %d", view.OfflineAfterSeconds)
		}
		excluded, err := view.Evaluate([]string{n.ID})
		if err != nil {
			t.Fatal(err)
		}
		if tc.reason == "" {
			if len(excluded) != 0 {
				t.Fatalf("unexpected exclusions: %+v", excluded)
			}
		} else if len(excluded) != 1 || excluded[0].Reason != tc.reason {
			t.Fatalf("exclusions = %+v, want %q", excluded, tc.reason)
		}
	}
}
