package backup

import (
	"database/sql"
	"testing"
)

// rewindNativeRuleSets removes migration 0143 (the native source format and the
// xray_url/xray_tag columns, plus the two native 国内 sources) so a fixture can
// stand in for an archive written before it.
func rewindNativeRuleSets(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0143_native_rule_sets.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DELETE FROM service_set_sources WHERE format='native'",
		"UPDATE service_sets SET description='内置：国内域名与国内 IP（meta-rules-dat geolocation-cn 与 geoip cn），用于屏蔽回国' WHERE id='service-cn' AND builtin=1 AND description LIKE '内置：国内域名与国内 IP（meta-rules-dat geolocation-cn 与 geoip cn；%'",
		"CREATE TABLE service_set_sources_old (id TEXT PRIMARY KEY, service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE, url TEXT NOT NULL, format TEXT NOT NULL CHECK(format IN ('yaml','text','mrs')), behavior TEXT NOT NULL CHECK(behavior IN ('classical','domain','ipcidr')), interval_seconds INTEGER NOT NULL DEFAULT 86400 CHECK(interval_seconds BETWEEN 3600 AND 604800), position INTEGER NOT NULL, last_status TEXT NOT NULL DEFAULT 'pending' CHECK(last_status IN ('pending','ok','failed')), last_error_code TEXT NOT NULL DEFAULT '', last_fetch_at TEXT, last_success_at TEXT, entry_count INTEGER NOT NULL DEFAULT 0, skipped_count INTEGER NOT NULL DEFAULT 0, encrypted_cache BLOB, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, CHECK(format <> 'mrs' OR behavior IN ('domain','ipcidr')))",
		"INSERT INTO service_set_sources_old(id,service_id,url,format,behavior,interval_seconds,position,last_status,last_error_code,last_fetch_at,last_success_at,entry_count,skipped_count,encrypted_cache,created_at,updated_at) SELECT id,service_id,url,format,behavior,interval_seconds,position,last_status,last_error_code,last_fetch_at,last_success_at,entry_count,skipped_count,encrypted_cache,created_at,updated_at FROM service_set_sources",
		"DROP TABLE service_set_sources",
		"ALTER TABLE service_set_sources_old RENAME TO service_set_sources",
		"CREATE INDEX service_set_sources_service ON service_set_sources(service_id, position)",
		"DELETE FROM schema_migrations WHERE version='0143_native_rule_sets.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}
