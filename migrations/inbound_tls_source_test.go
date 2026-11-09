package migrations

import (
	"database/sql"
	"testing"
)

// 0136 records where an inbound's certificate comes from. The column accepts the
// four sources plus the empty legacy value, and nothing else; the backfill of
// pre-0136 rows happens in the panel, because the certificate it would have to
// look at is inside the sealed settings column.
func TestInboundTLSSourceColumn(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// The migration alters an existing table, so the inbound row shape is stood
	// in for here with the columns the ALTER TABLE needs.
	if _, err := database.Exec("CREATE TABLE inbounds (id TEXT PRIMARY KEY, protocol TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	data, err := Files.ReadFile("0136_inbound_tls_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(string(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO inbounds(id,protocol) VALUES('legacy','vless')"); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := database.QueryRow("SELECT tls_source FROM inbounds WHERE id='legacy'").Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "" {
		t.Fatalf("default source = %q, want the legacy empty value", source)
	}
	for _, accepted := range []string{"paste", "self_signed", "file", "acme"} {
		if _, err := database.Exec("UPDATE inbounds SET tls_source=? WHERE id='legacy'", accepted); err != nil {
			t.Fatalf("rejected %q: %v", accepted, err)
		}
	}
	for _, refused := range []string{"pasted", "ACME", "self-signed", "none"} {
		if _, err := database.Exec("UPDATE inbounds SET tls_source=? WHERE id='legacy'", refused); err == nil {
			t.Fatalf("accepted invalid source %q", refused)
		}
	}
}
