package backup

import (
	"context"
	"errors"

	"testing"
)

func TestValidateTunnelImportTargets(t *testing.T) {
	s, _, _ := sourceFixture(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,created_at,updated_at) VALUES('import-source','source',x'01',60,'now','now')`,
		`INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES('import-target','import-source','target','http',x'01','now')`,
		`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_import_id,subscription_visible,enabled,status,created_at,updated_at) SELECT 'import-rule','import relay',id,23001,'manual','both','import','import-target',1,1,'draft','now','now' FROM nodes LIMIT 1`,
		`INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('import-group','group','subscription',1,1,'now')`,
		`INSERT INTO node_group_relays(group_id,rule_id) VALUES('import-group','import-rule')`,
	} {
		if _, err := s.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNodeGroupRelays(ctx, s.Database); err != nil {
		t.Fatalf("import relay membership rejected: %v", err)
	}
	if err := validateTunnelImportTargets(ctx, s.Database); err != nil {
		t.Fatalf("existing import target rejected: %v", err)
	}
	if _, err := s.Database.Exec("UPDATE tunnel_rules SET target_import_id=NULL"); err != nil {
		t.Fatal(err)
	}
	if err := validateTunnelImportTargets(ctx, s.Database); err != nil {
		t.Fatalf("deleted import target rejected: %v", err)
	}
	if _, err := s.Database.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("UPDATE tunnel_rules SET target_import_id='missing'"); err != nil {
		t.Fatal(err)
	}
	if err := validateTunnelImportTargets(ctx, s.Database); !errors.Is(err, ErrArchive) {
		t.Fatalf("dangling import target accepted: %v", err)
	}
}
