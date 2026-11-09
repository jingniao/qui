package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// 0135 adds the client connect address and the flag that protects a hand-typed
// public address. Until now only the Owner could set public_ip, so every stored
// value must come out marked manual: the Agent may fill only the empty ones.
func TestNodeConnectAddressMigrationBackfillsManualAddresses(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, statement := range []string{
		"CREATE TABLE nodes(id TEXT PRIMARY KEY, public_ip TEXT NOT NULL DEFAULT '')",
		"INSERT INTO nodes(id,public_ip) VALUES('typed','203.0.113.10')",
		"INSERT INTO nodes(id,public_ip) VALUES('empty','')",
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	apply(t, database, "0135_node_connect_address.sql")
	for _, test := range []struct {
		id         string
		manual     int
		connect    string
		wantManual int
	}{{id: "typed", wantManual: 1}, {id: "empty"}} {
		if err := database.QueryRow("SELECT public_ip_manual,connect_host FROM nodes WHERE id=?", test.id).Scan(&test.manual, &test.connect); err != nil {
			t.Fatal(err)
		}
		if test.manual != test.wantManual || test.connect != "" {
			t.Fatalf("%s: manual=%d connect=%q", test.id, test.manual, test.connect)
		}
	}
	// Only 0 and 1 are manual/auto; anything else is refused by the column.
	if _, err := database.Exec("UPDATE nodes SET public_ip_manual=2 WHERE id='typed'"); err == nil {
		t.Fatal("an out-of-range manual flag was accepted")
	}
}
