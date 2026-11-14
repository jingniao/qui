package migrations

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func TestOutboundSourceMigration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE outbounds(id TEXT); INSERT INTO outbounds VALUES('old')"); err != nil {
		t.Fatal(err)
	}
	apply(t, db, "0145_outbound_source.sql")
	var ip, iface string
	if err = db.QueryRow("SELECT source_ip,source_interface FROM outbounds").Scan(&ip, &iface); err != nil || ip != "" || iface != "" {
		t.Fatalf("%q %q %v", ip, iface, err)
	}
}
