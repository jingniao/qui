package recovery

import (
	"database/sql"
	"testing"
)

// rewindBuiltinRuleSets removes migration 0142 (data only: the built-in service
// sets, their sources and the 屏蔽回国 policy) so a fixture can stand in for an
// archive written before it.
func rewindBuiltinRuleSets(t *testing.T, database *sql.DB) {
	t.Helper()
	rewindNativeRuleSets(t, database)
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0142_builtin_rule_sets.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, query := range []string{
		"DELETE FROM policies WHERE id='policy-block-china'",
		"DELETE FROM service_set_sources WHERE id LIKE 'builtin-service-%'",
		"DELETE FROM service_sets WHERE builtin=1 AND id IN ('service-ads','service-ai','service-microsoft','service-apple','service-social','service-streaming','service-games','service-education','service-finance','service-cloud','service-spotify','service-pixiv','service-abema','service-ehentai','service-gfw','service-media','service-cn','service-private')",
		"UPDATE service_sets SET description='内置基础域名集合' WHERE builtin=1 AND description LIKE '内置：手写基础条目%'",
		"DELETE FROM schema_migrations WHERE version='0142_builtin_rule_sets.sql'",
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}
