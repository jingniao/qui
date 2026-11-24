package server

import (
	"context"
	"log/slog"
	"path/filepath"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/platform/db"
	"testing"
	"time"
)

func TestSpaceMaintenanceContinuesAfterCleanupFailure(t *testing.T) {
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	app := &Server{Database: database, accessRepository: &access.Repository{Database: database}, Logger: slog.Default()}
	// Use a separate payload table to produce real freelist pages, then break
	// only the retention job. The reclamation worker must still commit progress.
	if _, err := app.Database.Exec(`CREATE TABLE space_test_payload(value BLOB); WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<3000) INSERT INTO space_test_payload SELECT zeroblob(3500) FROM n; DELETE FROM space_test_payload; DROP TABLE access_retention_job`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.accessRepository.AdvanceRetention(context.Background(), time.Now()); err == nil {
		t.Fatal("expected genuine cleanup failure")
	}
	var before int
	if err := app.Database.QueryRow("PRAGMA freelist_count").Scan(&before); err != nil {
		t.Fatal(err)
	}
	observation, err := app.accessRepository.StorageObservation(context.Background())
	if err != nil || !observation.MainDatabaseReclaimPending {
		t.Fatalf("pending observation=%+v err=%v", observation, err)
	}
	for _, table := range observation.Tables {
		if (table.Kind == "core_samples" || table.Kind == "account_samples") && table.Retention != "one_day" {
			t.Fatalf("raw sample retention=%+v", table)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); app.runSpaceMaintenance(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("space worker did not drain free pages after cleanup failed")
		case <-ticker.C:
			var free int
			if err := app.Database.QueryRow("PRAGMA freelist_count").Scan(&free); err != nil {
				t.Fatal(err)
			}
			if free <= 32 {
				job, err := app.accessRepository.StorageObservation(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if job.Maintenance.Attempts < 2 || job.Maintenance.Status != "success" {
					t.Fatalf("maintenance=%+v before=%d free=%d", job.Maintenance, before, free)
				}
				if job.MainDatabaseReclaimPending {
					t.Fatal("drained freelist still reported pending")
				}
				return
			}
		}
	}
}
