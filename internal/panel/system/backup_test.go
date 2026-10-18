package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	"testing"
)

const backupTestPassword = "portable-system-backup-password"

func backupFixture(t *testing.T) *BackupService {
	t.Helper()
	root := t.TempDir()
	c := config.DefaultPanel()
	c.Database = filepath.Join(root, "panel.db")
	c.NodeIdentityDirectory = filepath.Join(root, "identity")
	database, err := db.Open(context.Background(), c.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := pki.LoadOrCreate(c.NodeIdentityDirectory); err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	r := &Repository{Database: database}
	return &BackupService{Repository: r, Source: archive.Source{Database: database, Config: c, ConfigurationVault: key}, Directory: filepath.Join(root, "backups")}
}

func TestBackupCatalogAuditVerificationAndFileDeletionStayConsistent(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	item, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := s.Repository.Backups(ctx, 1, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0] != item {
		t.Fatal("catalog missing successful file", total, err)
	}
	encoded, _ := json.Marshal(item)
	if bytes.Contains(encoded, []byte(backupTestPassword)) || bytes.Contains(encoded, []byte(s.Directory)) {
		t.Fatal("catalog disclosed password or host paths")
	}
	file, downloaded, err := s.Download(ctx, item.ID, "owner", "ip")
	if err != nil || downloaded != item {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || int64(len(data)) != item.FileBytes {
		t.Fatal("download did not return real artifact", err)
	}
	if _, err := s.Verify(ctx, item.ID, "owner", "ip", "different-system-password"); !errors.Is(err, archive.ErrArchive) {
		t.Fatal("wrong password verified", err)
	}
	unchanged, err := s.Repository.Backup(ctx, item.ID)
	if err != nil || unchanged.VerifiedAt != item.VerifiedAt {
		t.Fatal("failed verification changed audit state", err)
	}
	if _, err := s.Repository.Database.Exec("CREATE TRIGGER fail_backup_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, item.ID, "owner", "ip", backupTestPassword); err == nil {
		t.Fatal("verification reported success without committed audit")
	}
	unchanged, err = s.Repository.Backup(ctx, item.ID)
	if err != nil || unchanged.VerifiedAt != item.VerifiedAt {
		t.Fatal("verification audit failure left new timestamp", err)
	}
	if err := s.Delete(ctx, item.ID, "owner", "ip"); err == nil {
		t.Fatal("delete succeeded without audit")
	}
	file, err = os.Open(filepath.Join(s.Directory, item.ID+".pcb"))
	if err != nil {
		t.Fatal("failed delete did not restore original file", err)
	}
	file.Close()
	if _, err := s.Repository.Backup(ctx, item.ID); err != nil {
		t.Fatal("failed delete lost catalog", err)
	}
	if _, err := s.Repository.Database.Exec("DROP TRIGGER fail_backup_audit"); err != nil {
		t.Fatal(err)
	}
	verified, err := s.Verify(ctx, item.ID, "owner", "ip", backupTestPassword)
	if err != nil || verified.VerifiedAt == item.VerifiedAt {
		t.Fatal("actual content verification not persisted", err)
	}
	if err := s.Delete(ctx, item.ID, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Directory, item.ID+".pcb")); !os.IsNotExist(err) {
		t.Fatal("deleted file still present", err)
	}
	if _, total, err := s.Repository.Backups(ctx, 1, 20); err != nil || total != 0 {
		t.Fatal("deleted catalog remains", total, err)
	}
	var audits int
	if err := s.Repository.Database.QueryRow("SELECT count(*) FROM admin_operations").Scan(&audits); err != nil || audits != 4 {
		t.Fatal("unexpected real backup audit records", audits, err)
	}
}

func TestBackupAuditCreationFailurePublishesNeitherCatalogNorFile(t *testing.T) {
	s := backupFixture(t)
	if _, err := s.Repository.Database.Exec("CREATE TRIGGER fail_creation_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable');END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(context.Background(), "owner", "ip", backupTestPassword); err == nil {
		t.Fatal("creation succeeded without audit")
	}
	if _, total, err := s.Repository.Backups(context.Background(), 1, 20); err != nil || total != 0 {
		t.Fatal("audit failure left backup catalog", total, err)
	}
	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".store.lock" {
			t.Fatal("audit failure left archive or staging materials", entry.Name())
		}
	}
	if err := s.Recover(context.Background()); err != nil {
		t.Fatal("failed creation preparation could not recover", err)
	}
}

func TestBackupBusyTamperingAndMissingFilesFailExplicitly(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	s.mu.Lock()
	if _, err := s.Create(ctx, "owner", "ip", backupTestPassword); !errors.Is(err, ErrBackupBusy) {
		t.Fatal("overlapping backup admitted", err)
	}
	s.mu.Unlock()
	item, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Directory, item.ID+".pcb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if file, _, err := s.Download(ctx, item.ID, "owner", "ip"); !errors.Is(err, ErrBackupFile) || file != nil {
		t.Fatal("corrupt artifact downloaded", err)
	}
	if _, err := s.Verify(ctx, item.ID, "owner", "ip", backupTestPassword); !errors.Is(err, ErrBackupFile) {
		t.Fatal("corrupt artifact verified", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if file, _, err := s.Download(ctx, item.ID, "owner", "ip"); !errors.Is(err, ErrBackupFile) || file != nil {
		t.Fatal("missing artifact fabricated", err)
	}
	if err := s.Delete(ctx, item.ID, "owner", "ip"); err != nil {
		t.Fatal("missing artifact catalog could not be explicitly deleted", err)
	}
}
