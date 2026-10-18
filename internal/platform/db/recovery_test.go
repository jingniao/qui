package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAttachedRecoveryEvidenceReadOnlyAndCopyIdentifierValidation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	source, err := Open(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	candidate, err := Open(ctx, filepath.Join(root, "candidate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := AttachRecoverySource(ctx, candidate, sourcePath); err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec("INSERT INTO restore_source.system_settings(key,value_json) VALUES('changed','{}')"); err == nil {
		t.Fatal("recovery source attachment is writable")
	}
	tx, err := candidate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"backups;DELETE FROM admins", "1table", "restore_source.admins", ""} {
		if err := CopyRecoveryTable(ctx, tx, table, "id"); err == nil {
			t.Fatal("untrusted identifier accepted", table)
		}
	}
	if err := CopyRecoveryTable(ctx, tx, "backups", "id,created_at,created_by,kind,format_version,file_bytes,sha256,verified_at,origin"); err != nil {
		t.Fatal("valid digit-containing column rejected", err)
	}
}

func TestRecoveryWriteFreezeKeepsCommittedReadersAvailableAndBlocksOtherWriters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("INSERT INTO system_settings(key,value_json) VALUES('original','{}')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	freeze, err := FreezeForRecovery(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer freeze.Close()
	var count int64
	if err := database.QueryRow("SELECT count(*) FROM system_settings").Scan(&count); err != nil || count != 1 {
		t.Fatal("freeze blocked committed reader", err)
	}
	if _, err := database.Exec("INSERT INTO system_settings(key,value_json) VALUES('other-write','{}')"); err == nil {
		t.Fatal("writer entered frozen recovery source")
	}
	if second, err := FreezeForRecovery(ctx, path); err == nil || second != nil {
		t.Fatal("second freeze obtained write transaction", err)
	}
	if err := freeze.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO system_settings(key,value_json) VALUES('after-release','{}')"); err != nil {
		t.Fatal("freeze did not release writer", err)
	}
	if err := database.QueryRow("SELECT count(*) FROM system_settings").Scan(&count); err != nil || count != 2 {
		t.Fatal("freeze altered original source data", count, err)
	}
}
