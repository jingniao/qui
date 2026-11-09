package system

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/platform/db"
)

// TestRecordDailyWritesOncePerDay checks the rule that makes a repeating alert a
// daily reminder: one row per operator, action, target and UTC day, and a new row
// the next day or for another target.
func TestRecordDailyWritesOncePerDay(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &Repository{Database: database}
	for attempt, want := range []bool{true, false} {
		written, err := repository.RecordDaily(ctx, "system", "证书续期失败", "certificate-1")
		if err != nil || written != want {
			t.Fatalf("attempt %d: written %v want %v (%v)", attempt+1, written, want, err)
		}
	}
	var rows int
	if err := database.QueryRow("SELECT count(*) FROM admin_operations WHERE operator='system' AND action='证书续期失败' AND target='certificate-1'").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("rows: %d %v", rows, err)
	}
	// Another certificate is a different condition.
	if written, err := repository.RecordDaily(ctx, "system", "证书续期失败", "certificate-2"); err != nil || !written {
		t.Fatalf("second target: %v %v", written, err)
	}
	// Yesterday's row does not silence today's reminder.
	if _, err := database.Exec("UPDATE admin_operations SET timestamp=? WHERE target='certificate-1'", time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if written, err := repository.RecordDaily(ctx, "system", "证书续期失败", "certificate-1"); err != nil || !written {
		t.Fatalf("next day: %v %v", written, err)
	}
}
