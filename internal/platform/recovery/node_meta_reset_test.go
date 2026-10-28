package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
)

func TestRestoreKeepsCurrentResetBoundaryAndRenewalClaim(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		if _, err := f.source.Database.Exec("INSERT INTO node_meta(node_id,region,last_alert_day) VALUES(?,'JP','2026-10-27')", f.node.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("UPDATE users SET used_traffic=100,traffic_reset_cycle='monthly',traffic_reset_anchor='2024-01-31T00:00:00.000000000Z' WHERE id=?", f.user.ID); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec("UPDATE node_meta SET region='US',last_alert_day='2026-10-28'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Database.Exec("UPDATE users SET used_traffic=0,traffic_reset_at='2024-02-29T00:00:00.000000000Z' WHERE id=?", f.user.ID); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT used_traffic FROM users WHERE id=?", 0, f.user.ID)
	var region, day, cycle, anchor, last string
	if err = restored.QueryRow("SELECT region,last_alert_day FROM node_meta WHERE node_id=?", f.node.ID).Scan(&region, &day); err != nil {
		t.Fatal(err)
	}
	if region != "US" || day != "2026-10-28" {
		t.Fatal(region, day)
	}
	if err = restored.QueryRow("SELECT traffic_reset_cycle,traffic_reset_anchor,traffic_reset_at FROM users WHERE id=?", f.user.ID).Scan(&cycle, &anchor, &last); err != nil {
		t.Fatal(err)
	}
	if cycle != "monthly" || anchor != "2024-01-31T00:00:00.000000000Z" || last != "2024-02-29T00:00:00.000000000Z" {
		t.Fatal(cycle, anchor, last)
	}
}
