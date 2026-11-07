package migrations

import (
	"database/sql"
	"io/fs"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TEXT NOT NULL)"); err != nil {
		database.Close()
		t.Fatal(err)
	}
	names, err := fs.Glob(Files, "*.sql")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := fs.ReadFile(Files, name)
		if err != nil {
			database.Close()
			t.Fatal(err)
		}
		if _, err := database.Exec(string(data)); err != nil {
			database.Close()
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := database.Exec("INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)", name, "test", "now"); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	return database
}

func TestNodeIdentityRecoveryMigrationDefaultsAndChecks(t *testing.T) {
	database := migratedDatabase(t)
	defer database.Close()
	if _, err := database.Exec("INSERT INTO nodes(id,name,status,created_at) VALUES('n','node','offline','now')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO node_enrollment_tokens(token_hash,node_id,expires_at) VALUES(?,?,?)", "e", "n", "later"); err != nil {
		t.Fatal(err)
	}
	var purpose, bound string
	if err := database.QueryRow("SELECT purpose,bound_fingerprint FROM node_enrollment_tokens WHERE token_hash='e'").Scan(&purpose, &bound); err != nil {
		t.Fatal(err)
	}
	if purpose != "enroll" || bound != "" {
		t.Fatalf("defaults: %q %q", purpose, bound)
	}
	if _, err := database.Exec("UPDATE node_enrollment_tokens SET purpose='invalid' WHERE token_hash='e'"); err == nil {
		t.Fatal("purpose CHECK accepted invalid value")
	}
	if _, err := database.Exec("INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES('n','pem',?, 'later')", string(make([]byte, 64))); err != nil {
		// The identity fingerprint is intentionally not used by the recovery checks;
		// use a valid hexadecimal value for the recovery rows below.
		if _, err := database.Exec("INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES('n','pem',?, 'later')", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
			t.Fatal(err)
		}
	}
	validHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for i, id := range []string{"r1", "r2"} {
		_, err := database.Exec("INSERT INTO node_identity_recoveries(id,node_id,token_hash,previous_fingerprint,owner,revoked_on_issue,status,created_at,expires_at) VALUES(?,?,?,?,?,0,'issued','now','later')", id, "n", validHash[:63]+string(rune('0'+i)), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "owner")
		if i == 0 && err != nil {
			t.Fatal(err)
		}
		if i == 1 && err == nil {
			t.Fatal("issued recovery uniqueness constraint accepted duplicate")
		}
	}
	for _, statement := range []string{
		"UPDATE node_identity_recoveries SET revoked_on_issue=2 WHERE id='r1'",
		"UPDATE node_identity_recoveries SET status='invalid' WHERE id='r1'",
		"UPDATE node_identity_recoveries SET previous_fingerprint='short' WHERE id='r1'",
		"UPDATE node_identity_recoveries SET new_fingerprint='short' WHERE id='r1'",
		"UPDATE node_identity_recoveries SET redeploy_state='invalid' WHERE id='r1'",
		"INSERT INTO node_identity_retirements(fingerprint,node_id,recovery_id,retired_at) VALUES('short','n','r1','now')",
	} {
		if _, err := database.Exec(statement); err == nil {
			t.Fatalf("CHECK constraint accepted: %s", statement)
		}
	}
}
