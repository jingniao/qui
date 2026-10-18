package system

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"testing"
)

func TestImportedForeignBackupPreservesBytesOriginalKeysAndCurrentBusinessState(t *testing.T) {
	ctx := context.Background()
	source := backupFixture(t)
	target := backupFixture(t)
	item, err := source.Create(ctx, "source_owner", "source-ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source.Directory, item.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Repository.Database.Exec("INSERT INTO system_settings(key,value_json) VALUES('import_fixture','\"current-business-state\"')"); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(target.Source.Config.NodeIdentityDirectory, "configuration.key")
	currentKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	imported, created, err := target.Import(ctx, "target_owner", "target-ip", backupTestPassword, bytes.NewReader(data), func() error { return nil })
	if err != nil || !created || imported.ID != item.ID || imported.SHA256 != item.SHA256 || imported.CreatedAt != item.CreatedAt || imported.CreatedBy != "source_owner" || imported.Origin != "imported" {
		t.Fatal("foreign backup not registered faithfully", imported, created, err)
	}
	stored, err := os.ReadFile(filepath.Join(target.Directory, item.ID+".pcb"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("import rewrote portable archive", err)
	}
	if _, err := target.Verify(ctx, item.ID, "target_owner", "target-ip", backupTestPassword); err != nil {
		t.Fatal("foreign key materials could not verify on different panel", err)
	}
	stillKey, err := os.ReadFile(keyPath)
	if err != nil || !bytes.Equal(stillKey, currentKey) {
		t.Fatal("import replaced active key", err)
	}
	var current string
	if err := target.Repository.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='import_fixture'").Scan(&current); err != nil || current != `"current-business-state"` {
		t.Fatal("import overwrote live business data", err)
	}
	duplicate, created, err := target.Import(ctx, "target_owner", "target-ip", backupTestPassword, bytes.NewReader(data), func() error { return nil })
	if err != nil || created || duplicate.ID != item.ID {
		t.Fatal("repeat upload not idempotent", duplicate, created, err)
	}
	var audits int
	target.Repository.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='导入加密备份'").Scan(&audits)
	if audits != 1 {
		t.Fatal("duplicate upload duplicated success audit", audits)
	}
	file, _, err := target.Download(ctx, item.ID, "target_owner", "ip")
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(data, downloaded) {
		t.Fatal("import download was not original encrypted file", err)
	}
	if err := target.Delete(ctx, item.ID, "target_owner", "ip"); err != nil {
		t.Fatal("imported archive could not be deleted", err)
	}
}

func TestWrongPasswordTransportFailureAndAuditFailureDoNotRegisterImport(t *testing.T) {
	source := backupFixture(t)
	item, err := source.Create(context.Background(), "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source.Directory, item.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"wrong-password", "truncated", "completion", "audit"} {
		t.Run(fault, func(t *testing.T) {
			target := backupFixture(t)
			payload := data
			password := backupTestPassword
			complete := func() error { return nil }
			switch fault {
			case "wrong-password":
				password = "wrong-import-password"
			case "truncated":
				payload = payload[:len(payload)/2]
			case "completion":
				complete = func() error { return archive.ErrReceive }
			case "audit":
				if _, err := target.Repository.Database.Exec("CREATE TRIGGER fail_import_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := target.Import(context.Background(), "owner", "ip", password, bytes.NewReader(payload), complete); err == nil {
				t.Fatal("invalid import succeeded", fault)
			}
			if _, total, err := target.Repository.Backups(context.Background(), 1, 20); err != nil || total != 0 {
				t.Fatal("failed import left catalog", total, err)
			}
			if _, err := os.Stat(filepath.Join(target.Directory, item.ID+".pcb")); !os.IsNotExist(err) {
				t.Fatal("failed import left final archive", err)
			}
			if err := target.Recover(context.Background()); err != nil {
				t.Fatal("failed import journal could not recover", err)
			}
			entries, err := os.ReadDir(target.Directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != ".store.lock" {
					t.Fatal("failed import left staging", entry.Name())
				}
			}
		})
	}
}

func TestSameIdentifierDifferentAuthenticatedContentConflictsAndNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	source := backupFixture(t)
	target := backupFixture(t)
	item, err := source.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source.Directory, item.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := target.Import(ctx, "owner", "ip", backupTestPassword, bytes.NewReader(data), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	other := backupFixture(t)
	if _, err := other.Source.Create(ctx, other.Directory, item.ID, "owner", backupTestPassword); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(filepath.Join(other.Directory, item.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := target.Import(ctx, "owner", "ip", backupTestPassword, bytes.NewReader(changed), func() error { return nil }); !errors.Is(err, ErrBackupImportConflict) {
		t.Fatal("different archive sharing identity accepted", err)
	}
	stored, err := os.ReadFile(filepath.Join(target.Directory, item.ID+".pcb"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("conflicting upload overwrote original archive", err)
	}
	if _, total, err := target.Repository.Backups(ctx, 1, 20); err != nil || total != 1 {
		t.Fatal("conflicting import altered catalog", total, err)
	}
}
