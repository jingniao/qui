package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/filelock"
	"time"
)

var ErrBackupRecovery = errors.New("backup store requires consistent recovery before further operations")

func (s *BackupService) beginStore(ctx context.Context) (func(), error) {
	if !s.mu.TryLock() {
		return nil, ErrBackupBusy
	}
	if info, err := os.Lstat(s.Directory); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		s.mu.Unlock()
		return nil, ErrBackupRecovery
	} else if err != nil && !os.IsNotExist(err) {
		s.mu.Unlock()
		return nil, err
	}
	lock, err := filelock.Try(filepath.Join(s.Directory, ".store.lock"))
	if err != nil {
		s.mu.Unlock()
		if errors.Is(err, filelock.ErrBusy) {
			return nil, ErrBackupBusy
		}
		return nil, err
	}
	if err := s.recoverFilesLocked(ctx); err != nil {
		lock.Close()
		s.mu.Unlock()
		return nil, err
	}
	return func() { lock.Close(); s.mu.Unlock() }, nil
}

func (r *Repository) prepareBackupFile(ctx context.Context, item Backup, operation string) error {
	if !validBackupID(item.ID) || operation != "create" && operation != "delete" {
		return ErrBackupRecovery
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = r.Database.ExecContext(ctx, "INSERT INTO backup_file_ops(backup_id,operation,phase,metadata_json,created_at) VALUES(?,?,'prepared',?,?)", item.ID, operation, string(data), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (r *Repository) forgetBackupFile(ctx context.Context, id string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM backup_file_ops WHERE backup_id=?", id)
	return err
}

func (s *BackupService) recoverFilesLocked(ctx context.Context) error {
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT backup_id,operation,phase,metadata_json FROM backup_file_ops ORDER BY created_at,backup_id")
	if err != nil {
		return err
	}
	type pending struct {
		id, operation, phase string
		item                 Backup
	}
	operations := []pending{}
	for rows.Next() {
		var entry pending
		var data string
		if err := rows.Scan(&entry.id, &entry.operation, &entry.phase, &data); err != nil {
			rows.Close()
			return err
		}
		decoder := json.NewDecoder(bytes.NewBufferString(data))
		decoder.DisallowUnknownFields()
		if !validBackupID(entry.id) || len(data) > 8192 || decoder.Decode(&entry.item) != nil || decoder.Decode(&struct{}{}) != io.EOF || entry.item.ID != entry.id {
			rows.Close()
			return ErrBackupRecovery
		}
		if entry.operation == "delete" && entry.item.Origin == "" {
			entry.item.Origin = "local"
		} // Preparations made before migration 0021.
		operations = append(operations, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, entry := range operations {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, readErr := s.Repository.Backup(ctx, entry.id)
		if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		path := filepath.Join(s.Directory, entry.id+".pcb")
		trash := filepath.Join(s.Directory, ".trash-"+entry.id)
		fileInfo, fileErr := os.Lstat(path)
		trashInfo, trashErr := os.Lstat(trash)
		if fileErr != nil && !os.IsNotExist(fileErr) || trashErr != nil && !os.IsNotExist(trashErr) || fileErr == nil && !fileInfo.Mode().IsRegular() || trashErr == nil && !trashInfo.Mode().IsRegular() {
			return ErrBackupRecovery
		}
		switch entry.operation {
		case "create":
			if entry.phase != "prepared" || trashErr == nil || entry.item != (Backup{ID: entry.id}) {
				return ErrBackupRecovery
			}
			if readErr == nil {
				return ErrBackupRecovery
			} // Successful catalog commit removes its preparation atomically.
			if fileErr == nil {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		case "delete":
			if entry.phase == "prepared" {
				if readErr != nil || current != entry.item {
					return ErrBackupRecovery
				}
				if fileErr == nil && trashErr == nil {
					return ErrBackupRecovery
				}
				if trashErr == nil {
					if err := os.Rename(trash, path); err != nil {
						return err
					}
				}
			} else if entry.phase == "committed" {
				if readErr == nil || fileErr == nil {
					return ErrBackupRecovery
				}
				if trashErr == nil {
					if err := os.Remove(trash); err != nil {
						return err
					}
				}
			} else {
				return ErrBackupRecovery
			}
		default:
			return ErrBackupRecovery
		}
		if err := s.Repository.forgetBackupFile(ctx, entry.id); err != nil {
			return err
		}
	}
	return archive.CleanupStages(s.Directory)
}

// Recover owns the process lock; it never reconstructs catalog rows from files.
func (s *BackupService) Recover(ctx context.Context) error {
	done, err := s.beginStore(ctx)
	if err != nil {
		return err
	}
	defer done()
	return nil
}
