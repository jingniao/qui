package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNodeTagsMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("CREATE TABLE nodes(id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO nodes(id) VALUES('node-1'),('node-2')"); err != nil {
		t.Fatal(err)
	}
	apply(t, database, "0117_node_meta.sql")
	if _, err := database.Exec("INSERT INTO node_meta(node_id,region) VALUES('node-1','HK')"); err != nil {
		t.Fatal(err)
	}
	apply(t, database, "0144_node_tags.sql")

	var tags string
	if err := database.QueryRow("SELECT tags_json FROM node_meta WHERE node_id='node-1'").Scan(&tags); err != nil || tags != "[]" {
		t.Fatalf("existing row tags = %q, %v", tags, err)
	}
	if _, err := database.Exec(`INSERT INTO node_meta(node_id,tags_json) VALUES('node-2','["香港","CN2"]')`); err != nil {
		t.Fatalf("valid tags refused: %v", err)
	}
	for _, bad := range []string{"", "{}", `"x"`, "[", "null"} {
		if _, err := database.Exec("UPDATE node_meta SET tags_json=? WHERE node_id='node-1'", bad); err == nil {
			t.Fatalf("tags_json %q accepted", bad)
		}
	}
}
