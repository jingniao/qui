package migrations

import (
	"database/sql"
	"testing"
)

func TestCanvasForwardMigrationMarker(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	data, err := Files.ReadFile("0131_canvas_forward_migration.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(string(data)); err != nil {
		t.Fatal(err)
	}
	var done int
	if err = database.QueryRow("SELECT done FROM canvas_forward_reconcile WHERE id=1").Scan(&done); err != nil || done != 0 {
		t.Fatal(done, err)
	}
	for _, query := range []string{"UPDATE canvas_forward_reconcile SET done=2", "INSERT INTO canvas_forward_reconcile VALUES(2,0)", "INSERT INTO tunnel_migration_report VALUES('id','name',NULL)"} {
		if _, err = database.Exec(query); err == nil {
			t.Fatal("accepted invalid marker/report", query)
		}
	}
}
