package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestTuningRecommendationsMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}

	for _, statement := range []string{
		"CREATE TABLE nodes(id TEXT PRIMARY KEY)",
		"INSERT INTO nodes(id) VALUES('node-1')",
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	apply(t, database, "0117_node_meta.sql")
	apply(t, database, "0123_node_metrics_tuning.sql")
	apply(t, database, "0141_tuning_recommendations.sql")

	// 1. node_metric_samples has tcp_out_segs and tcp_retrans_segs
	if _, err := database.Exec("INSERT INTO node_metric_samples(node_id,at,tcp_out_segs,tcp_retrans_segs) VALUES('node-1','2026-11-10T00:00:00Z',1000,12)"); err != nil {
		t.Fatalf("insert into node_metric_samples failed: %v", err)
	}

	// 2. node_meta bandwidth_mbps CHECK constraint (NULL or 1..100000)
	if _, err := database.Exec("INSERT INTO node_meta(node_id,bandwidth_mbps) VALUES('node-1',NULL)"); err != nil {
		t.Fatalf("expected NULL bandwidth_mbps to succeed: %v", err)
	}
	if _, err := database.Exec("UPDATE node_meta SET bandwidth_mbps=1000 WHERE node_id='node-1'"); err != nil {
		t.Fatalf("expected bandwidth_mbps=1000 to succeed: %v", err)
	}
	for _, bad := range []int{0, -10, 100001} {
		if _, err := database.Exec("UPDATE node_meta SET bandwidth_mbps=? WHERE node_id='node-1'", bad); err == nil {
			t.Fatalf("expected bandwidth_mbps=%d to fail CHECK constraint", bad)
		}
	}

	// 3. node_tuning_dismissals and ON DELETE CASCADE
	if _, err := database.Exec("INSERT INTO node_tuning_dismissals(node_id,key,value,rules_version,dismissed_at) VALUES('node-1','net.ipv4.tcp_congestion_control','bbr','2026.11-1','2026-11-10T00:00:00Z')"); err != nil {
		t.Fatalf("insert into node_tuning_dismissals failed: %v", err)
	}
	if _, err := database.Exec("DELETE FROM nodes WHERE id='node-1'"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow("SELECT count(*) FROM node_tuning_dismissals WHERE node_id='node-1'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected cascade delete on node_tuning_dismissals, got %d rows", count)
	}
}
