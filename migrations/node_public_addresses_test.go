package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNodePublicAddressesMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, statement := range []string{
		"CREATE TABLE nodes(id TEXT PRIMARY KEY, public_ip TEXT NOT NULL DEFAULT '', public_ip_manual INTEGER NOT NULL DEFAULT 0)",
		"INSERT INTO nodes(id,public_ip,public_ip_manual) VALUES('manual','203.0.113.10',1)",
		"INSERT INTO nodes(id,public_ip,public_ip_manual) VALUES('auto','198.51.100.5',0)",
		"INSERT INTO nodes(id,public_ip,public_ip_manual) VALUES('empty','',1)",
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	apply(t, database, "0138_node_public_addresses.sql")

	for _, test := range []struct {
		id           string
		wantSelected string
		wantJSON     string
	}{
		{id: "manual", wantSelected: "203.0.113.10", wantJSON: "[]"},
		{id: "auto", wantSelected: "", wantJSON: "[]"},
		{id: "empty", wantSelected: "", wantJSON: "[]"},
	} {
		var selected, jsonStr string
		if err := database.QueryRow("SELECT public_ip_selected, public_addresses_json FROM nodes WHERE id=?", test.id).Scan(&selected, &jsonStr); err != nil {
			t.Fatal(err)
		}
		if selected != test.wantSelected || jsonStr != test.wantJSON {
			t.Fatalf("%s: selected=%q (want %q), json=%q (want %q)", test.id, selected, test.wantSelected, jsonStr, test.wantJSON)
		}
	}
}
