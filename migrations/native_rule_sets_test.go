package migrations

import (
	"database/sql"
	"testing"
)

func nativeRuleSetsBase(t *testing.T) *sql.DB {
	t.Helper()
	database := builtinRuleSetsBase(t)
	apply(t, database, "0142_builtin_rule_sets.sql")
	return database
}

func TestNativeRuleSetsMigrationFresh(t *testing.T) {
	database := nativeRuleSetsBase(t)
	if _, err := database.Exec("UPDATE service_set_sources SET last_status='ok', entry_count=7, encrypted_cache=x'0102' WHERE id='builtin-service-cn-1'"); err != nil {
		t.Fatal(err)
	}
	before := countRows(t, database, "SELECT count(*) FROM service_set_sources")
	var yamlURL string
	var yamlPos int
	if err := database.QueryRow("SELECT url, position FROM service_set_sources WHERE id='builtin-service-cn-1'").Scan(&yamlURL, &yamlPos); err != nil {
		t.Fatal(err)
	}
	var maxYaml int
	if err := database.QueryRow("SELECT max(position) FROM service_set_sources WHERE service_id='service-cn'").Scan(&maxYaml); err != nil {
		t.Fatal(err)
	}
	applyWhole(t, database, "0143_native_rule_sets.sql")
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources"); got != before+2 {
		t.Fatalf("sources = %d, want %d", got, before+2)
	}
	type native struct {
		url, behavior, xrayURL, xrayTag, status string
		position, interval                      int
	}
	want := map[string]native{
		"builtin-service-cn-native-1": {"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs", "domain", "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat", "cn", "pending", maxYaml + 1, 86400},
		"builtin-service-cn-native-2": {"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs", "ipcidr", "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat", "cn", "pending", maxYaml + 2, 86400},
	}
	for id, expected := range want {
		var got native
		var format string
		var cache []byte
		if err := database.QueryRow("SELECT url, format, behavior, xray_url, xray_tag, last_status, position, interval_seconds, encrypted_cache FROM service_set_sources WHERE id=? AND service_id='service-cn'", id).
			Scan(&got.url, &format, &got.behavior, &got.xrayURL, &got.xrayTag, &got.status, &got.position, &got.interval, &cache); err != nil {
			t.Fatal(id, err)
		}
		if got != expected || format != "native" || cache != nil {
			t.Fatalf("%s = %+v format=%s cache=%v, want %+v", id, got, format, cache, expected)
		}
	}
	// The rebuild keeps every column of the existing sources.
	var url, status string
	var position, entries int
	var cache []byte
	if err := database.QueryRow("SELECT url, position, last_status, entry_count, encrypted_cache FROM service_set_sources WHERE id='builtin-service-cn-1'").Scan(&url, &position, &status, &entries, &cache); err != nil {
		t.Fatal(err)
	}
	if url != yamlURL || position != yamlPos || status != "ok" || entries != 7 || len(cache) != 2 {
		t.Fatal(url, position, status, entries, cache)
	}
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources WHERE format<>'native' AND (xray_url<>'' OR xray_tag<>'')"); got != 0 {
		t.Fatalf("non-native sources with xray fields = %d", got)
	}
	// The CHECKs: native needs both Xray fields and no cache; others none.
	for _, query := range []string{
		"INSERT INTO service_set_sources(id,service_id,url,format,behavior,position,created_at,updated_at) VALUES('x1','service-cn','https://a','native','domain',9,'t','t')",
		"INSERT INTO service_set_sources(id,service_id,url,format,behavior,position,created_at,updated_at,xray_url,xray_tag) VALUES('x2','service-cn','https://a','native','classical',9,'t','t','https://b','cn')",
		"INSERT INTO service_set_sources(id,service_id,url,format,behavior,position,created_at,updated_at,xray_url,xray_tag) VALUES('x3','service-cn','https://a','yaml','domain',9,'t','t','https://b','cn')",
		"INSERT INTO service_set_sources(id,service_id,url,format,behavior,position,created_at,updated_at,xray_url,xray_tag,encrypted_cache) VALUES('x4','service-cn','https://a','native','domain',9,'t','t','https://b','cn',x'01')",
	} {
		if _, err := database.Exec(query); err == nil {
			t.Fatalf("CHECK accepted %s", query)
		}
	}
}

func TestNativeRuleSetsMigrationSkipsCustomSets(t *testing.T) {
	// Not the built-in set: owner-created service-cn, no native sources added.
	database := builtinRuleSetsBase(t)
	if _, err := database.Exec("DELETE FROM service_sets WHERE id='service-cn'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO service_sets(id,name,builtin,version,enabled,description,created_at,updated_at) VALUES('service-cn','自建',0,1,1,'','t','t')"); err != nil {
		t.Fatal(err)
	}
	apply(t, database, "0142_builtin_rule_sets.sql")
	applyWhole(t, database, "0143_native_rule_sets.sql")
	if got := countRows(t, database, "SELECT count(*) FROM service_set_sources WHERE format='native'"); got != 0 {
		t.Fatalf("native sources added to a custom set: %d", got)
	}
}

// applyWhole runs a migration as one script, like the production runner. The
// shared apply helper splits on semicolons, which 0143's header comment
// contains.
func applyWhole(t *testing.T, database *sql.DB, name string) {
	t.Helper()
	data, err := Files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(string(data)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}
