package backup

import (
	"context"
	"errors"
	"path/filepath"
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
	if _, err := s.Database.Exec("UPDATE tunnel_rules SET target_import_id=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("DELETE FROM schema_migrations WHERE version='0152_tunnel_rule_import_target.sql'"); err != nil {
		t.Fatal(err)
	}
	if err := validateTunnelImportTargets(ctx, s.Database); !errors.Is(err, ErrArchive) {
		t.Fatalf("0152 column without migration accepted: %v", err)
	}
}

func Test0151ArchiveStillValidatesAndPreparesRestore(t *testing.T) {
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	rewindTunnelImportTarget(t, s.Database)
	if err := validateTunnelImportTargets(ctx, s.Database); err != nil {
		t.Fatalf("0151 archive rejected: %v", err)
	}
	created, err := s.Create(ctx, directory, "archive_0151_restore", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, restoreTargetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
}
