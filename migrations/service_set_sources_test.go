package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestServiceSetSourcesMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}

	apply(t, database, "0009_policy.sql")
	apply(t, database, "0140_service_set_sources.sql")

	// 1. Verify existing service_set_entries are preserved
	var entryCount int
	if err := database.QueryRow("SELECT count(*) FROM service_set_entries").Scan(&entryCount); err != nil {
		t.Fatal(err)
	}
	if entryCount == 0 {
		t.Fatal("expected seed service_set_entries to be preserved")
	}

	// 2. Can insert 'keyword' kind into service_set_entries
	var serviceID string
	if err := database.QueryRow("SELECT id FROM service_sets LIMIT 1").Scan(&serviceID); err != nil {
		t.Fatal(err)
	}

	if _, err := database.Exec("INSERT INTO service_set_entries(service_id,kind,value) VALUES(?, 'keyword', 'openai')", serviceID); err != nil {
		t.Fatalf("failed to insert keyword entry: %v", err)
	}

	// 3. mrs + classical rejected by CHECK constraint
	if _, err := database.Exec("INSERT INTO service_set_sources(id,service_id,url,format,behavior,interval_seconds,position,created_at,updated_at) VALUES('src1',?,'https://example.com/rules.mrs','mrs','classical',86400,0,'2026-11-09T00:00:00Z','2026-11-09T00:00:00Z')", serviceID); err == nil {
		t.Fatal("expected CHECK constraint failure for mrs + classical")
	}

	// 4. Valid source can be inserted
	if _, err := database.Exec("INSERT INTO service_set_sources(id,service_id,url,format,behavior,interval_seconds,position,created_at,updated_at) VALUES('src1',?,'https://example.com/rules.mrs','mrs','domain',86400,0,'2026-11-09T00:00:00Z','2026-11-09T00:00:00Z')", serviceID); err != nil {
		t.Fatalf("failed to insert valid mrs + domain source: %v", err)
	}

	// 5. Deleting service_set cascades to delete service_set_sources
	if _, err := database.Exec("DELETE FROM service_sets WHERE id=?", serviceID); err != nil {
		t.Fatal(err)
	}
	var srcCount int
	if err := database.QueryRow("SELECT count(*) FROM service_set_sources WHERE service_id=?", serviceID).Scan(&srcCount); err != nil {
		t.Fatal(err)
	}
	if srcCount != 0 {
		t.Fatalf("expected cascade deletion of sources, got count=%d", srcCount)
	}
}
