package migrations

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func builtinRuleSetsBase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	apply(t, database, "0009_policy.sql")
	apply(t, database, "0140_service_set_sources.sql")
	return database
}

func countRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var value int
	if err := database.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestBuiltinRuleSetsMigrationFresh(t *testing.T) {
	database := builtinRuleSetsBase(t)
	apply(t, database, "0142_builtin_rule_sets.sql")
	if got := countRows(t, database, "SELECT count(*) FROM service_sets WHERE builtin=1"); got != 25 {
		t.Fatalf("built-in sets = %d, want 25", got)
	}
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources"); got != 63 {
		t.Fatalf("sources = %d, want 63", got)
	}
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources WHERE service_id='service-cn'"); got != 2 {
		t.Fatalf("国内 sources = %d", got)
	}
	rows, err := database.Query("SELECT url, format, behavior, last_status FROM service_set_sources")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var url, format, behavior, status string
		if err := rows.Scan(&url, &format, &behavior, &status); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(url, "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/") || !strings.HasSuffix(url, ".yaml") || format != "yaml" || status != "pending" {
			t.Fatal(url, format, status)
		}
		if strings.Contains(url, "/geoip/") != (behavior == "ipcidr") {
			t.Fatal("behavior does not match the list kind", url, behavior)
		}
	}
	var enabled, priority int
	var match, action string
	if err := database.QueryRow("SELECT enabled, priority, match_json, action_json FROM policies WHERE id='policy-block-china'").Scan(&enabled, &priority, &match, &action); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || priority != 1 || match != `{"service_ids":["service-cn"]}` || !strings.Contains(action, `"type":"Block"`) {
		t.Fatal(enabled, priority, match, action)
	}
	var description string
	if err := database.QueryRow("SELECT description FROM service_sets WHERE id='service-netflix'").Scan(&description); err != nil || !strings.HasPrefix(description, "内置：手写基础条目") {
		t.Fatal(description, err)
	}
	// The hand-written entries of the 0009 sets stay.
	if got := countRows(t, database, "SELECT count(*) FROM service_set_entries WHERE service_id='service-netflix'"); got != 4 {
		t.Fatalf("netflix entries = %d", got)
	}
}

// An Owner's custom set of the same name and an Owner's own source on a
// built-in set are left alone; the policy needs the built-in 国内 set.
func TestBuiltinRuleSetsMigrationKeepsOwnerData(t *testing.T) {
	database := builtinRuleSetsBase(t)
	for _, statement := range []string{
		"INSERT INTO service_sets(id,name,builtin,enabled,description,created_at,updated_at) VALUES('custom-1','国内',0,1,'','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"INSERT INTO service_set_sources(id,service_id,url,format,behavior,position,created_at,updated_at) VALUES('own','service-google','https://rules.example/google.txt','text','domain',0,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
		"UPDATE service_sets SET description='我的说明' WHERE id='service-youtube'",
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	apply(t, database, "0142_builtin_rule_sets.sql")
	if got := countRows(t, database, "SELECT count(*) FROM service_sets WHERE id='service-cn'"); got != 0 {
		t.Fatal("built-in set created over a custom set of the same name")
	}
	if got := countRows(t, database, "SELECT count(*) FROM policies WHERE id='policy-block-china'"); got != 0 {
		t.Fatal("policy created without the built-in 国内 set")
	}
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources WHERE service_id='service-google'"); got != 1 {
		t.Fatalf("Owner's google sources changed: %d", got)
	}
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources WHERE service_id='service-youtube'"); got != 1 {
		t.Fatalf("youtube sources = %d", got)
	}
	var description string
	if err := database.QueryRow("SELECT description FROM service_sets WHERE id='service-youtube'").Scan(&description); err != nil || description != "我的说明" {
		t.Fatal("Owner's description overwritten", description, err)
	}
	if got := countRows(t, database, "SELECT count(*) FROM sqlite_temp_master WHERE name='m0142_targets'"); got != 0 {
		t.Fatal("temporary table left behind")
	}
}
