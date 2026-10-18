package proxy

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/migrations"
	"testing"
	"testing/fstest"
	"time"
)

func TestFallbackMigrationPreservesLegacyModesAndEncryptedSettings(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(directory, "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	legacy := fstest.MapFS{}
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "0013_fallback_targets.sql" {
			continue
		}
		data, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		legacy[name] = &fstest.MapFile{Data: data}
	}
	if err := db.Migrate(ctx, database, legacy); err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Legacy", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	configurationVault, err := vault.Open(filepath.Join(directory, "key"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := configurationVault.Seal([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ id, mode, backup string }{{"backup", "Block", ""}, {"primary", "BackupOutbound", "backup"}} {
		if _, err := database.ExecContext(ctx, `INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,created_at) VALUES(?,?,?,'socks5','192.0.2.10',1080,1,?,1,1,?,?,?)`, value.id, value.id, n.ID, sealed, value.mode, value.backup, timestamp()); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	r := &Repository{Database: database, Vault: configurationVault}
	out, err := r.Outbound(ctx, "primary", false)
	if err != nil || out.FailurePolicy != "BackupOutbound" || out.BackupOutboundID != "backup" {
		t.Fatal("legacy mode or reference changed", out, err)
	}
	var stored []byte
	if err := database.QueryRowContext(ctx, "SELECT encrypted_settings FROM outbounds WHERE id='primary'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, sealed) {
		t.Fatal("migration rewrote encrypted settings")
	}
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal("migration not repeatable", err)
	}
}
