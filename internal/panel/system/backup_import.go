package system

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"time"
)

var ErrBackupImportConflict = errors.New("backup identifier already exists with different content")

// Import keeps the authenticated archive bytes and identity unchanged. Only
// catalog registration occurs here; no live business state is restored.
func (s *BackupService) Import(ctx context.Context, actor, ip, password string, source io.Reader, complete func() error) (item Backup, created bool, resultErr error) {
	if err := archive.ValidatePassword(password); err != nil {
		return item, false, err
	}
	if actor == "" || len(actor) > 128 || complete == nil {
		return item, false, archive.ErrReceive
	}
	done, err := s.beginStore(ctx)
	if err != nil {
		return item, false, err
	}
	defer done()
	_, err = archive.Receive(ctx, s.Directory, source, password, func(result archive.Result, receivedPath string) error {
		// Consume the multipart ending before any publication. Extra parts or
		// transport errors cannot be hidden behind a valid first archive.
		if err := complete(); err != nil {
			return err
		}
		if !validBackupID(result.Manifest.ID) {
			return archive.ErrReceive
		}
		existing, err := s.Repository.Backup(ctx, result.Manifest.ID)
		if err == nil {
			if existing.SHA256 != result.SHA256 || existing.FileBytes != result.Bytes || existing.CreatedBy != result.Manifest.CreatedBy || existing.CreatedAt != result.Manifest.CreatedAt.Format(time.RFC3339Nano) {
				return ErrBackupImportConflict
			}
			file, err := s.openChecked(ctx, existing)
			if err != nil {
				return err
			}
			file.Close()
			item = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		item = Backup{ID: result.Manifest.ID, CreatedAt: result.Manifest.CreatedAt.Format(time.RFC3339Nano), CreatedBy: result.Manifest.CreatedBy, Kind: "manual", FormatVersion: 1, FileBytes: result.Bytes, SHA256: result.SHA256, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Origin: "imported"}
		final := filepath.Join(s.Directory, item.ID+".pcb")
		if _, err := os.Lstat(final); !os.IsNotExist(err) {
			return ErrBackupImportConflict
		}
		if err := s.Repository.prepareBackupFile(ctx, Backup{ID: item.ID}, "create"); err != nil {
			return err
		}
		if err := os.Link(receivedPath, final); err != nil {
			return err
		}
		if err := s.Repository.saveBackup(ctx, item, actor, ip, nil); err != nil {
			return errors.Join(err, os.Remove(final))
		}
		created = true
		if s.Notify != nil {
			s.Notify()
		}
		return nil
	})
	if err != nil {
		return Backup{}, false, err
	}
	return item, created, nil
}
