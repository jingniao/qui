package system

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/id"
	"sync"
	"time"
)

var ErrBackupBusy = errors.New("another backup operation is in progress")
var ErrBackupFile = errors.New("backup file is missing or does not match its catalog")

type Backup struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"createdAt"`
	CreatedBy     string `json:"createdBy"`
	Kind          string `json:"kind"`
	FormatVersion int    `json:"formatVersion"`
	FileBytes     int64  `json:"fileBytes"`
	SHA256        string `json:"sha256"`
	VerifiedAt    string `json:"verifiedAt"`
	Origin        string `json:"origin"`
}

// BackupService serializes bounded filesystem operations. Catalog and audit
// writes stay in the System repository's SQLite transaction.
type BackupService struct {
	Repository *Repository
	Source     archive.Source
	Directory  string
	mu         sync.Mutex
	Notify     func()
}

func validBackupID(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func (s *BackupService) Create(ctx context.Context, actor, ip, password string) (Backup, error) {
	if err := archive.ValidatePassword(password); err != nil {
		return Backup{}, err
	}
	done, err := s.beginStore(ctx)
	if err != nil {
		return Backup{}, err
	}
	defer done()
	return s.createLocked(ctx, actor, ip, password, "manual", nil)
}

func (s *BackupService) createLocked(ctx context.Context, actor, ip, password, kind string, claim *backupClaim) (Backup, error) {
	backupID, err := id.New()
	if err != nil {
		return Backup{}, err
	}
	if err := s.Repository.prepareBackupFile(ctx, Backup{ID: backupID}, "create"); err != nil {
		return Backup{}, err
	}
	result, err := s.Source.Create(ctx, s.Directory, backupID, actor, password)
	if err != nil {
		return Backup{}, err
	}
	item := Backup{ID: backupID, CreatedAt: result.Manifest.CreatedAt.Format(time.RFC3339Nano), CreatedBy: actor, Kind: kind, FormatVersion: 1, FileBytes: result.Bytes, SHA256: result.SHA256, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Origin: "local"}
	if err := s.Repository.saveBackup(ctx, item, actor, ip, claim); err != nil {
		removeErr := os.Remove(filepath.Join(s.Directory, backupID+".pcb"))
		return Backup{}, errors.Join(err, removeErr)
	}
	if s.Notify != nil {
		s.Notify()
	}
	return item, nil
}

func (r *Repository) saveBackup(ctx context.Context, item Backup, actor, ip string, claim *backupClaim) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if claim != nil {
		current, err := readBackupSchedule(ctx, tx)
		if err != nil {
			return err
		}
		if !current.Policy.Enabled || current.Revision != claim.ScheduleRevision {
			return ErrBackupScheduleConflict
		}
	}
	deleted, err := tx.ExecContext(ctx, "DELETE FROM backup_file_ops WHERE backup_id=? AND operation='create' AND phase='prepared'", item.ID)
	if err != nil {
		return err
	}
	count, err := deleted.RowsAffected()
	if err != nil || count != 1 {
		return ErrBackupRecovery
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO backups(id,created_at,created_by,kind,format_version,file_bytes,sha256,verified_at,origin) VALUES(?,?,?,?,?,?,?,?,?)", item.ID, item.CreatedAt, item.CreatedBy, item.Kind, item.FormatVersion, item.FileBytes, item.SHA256, item.VerifiedAt, item.Origin); err != nil {
		return err
	}
	if claim != nil {
		result, err := tx.ExecContext(ctx, "UPDATE backup_runs SET status='success',backup_id=?,lease_until='',retry_at='',error_code='' WHERE id=? AND claim_token=? AND status='running'", item.ID, claim.ID, claim.Token)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			return ErrBackupScheduleConflict
		}
	}
	action := "创建系统备份"
	if item.Origin == "imported" {
		action = "导入加密备份"
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,'成功',?)", time.Now().UTC().Format(time.RFC3339Nano), actor, action, item.ID, ip); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Backups(ctx context.Context, page, size int) ([]Backup, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM backups").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,created_at,created_by,kind,format_version,file_bytes,sha256,verified_at,origin FROM backups ORDER BY created_at DESC,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Backup{}
	for rows.Next() {
		var item Backup
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.CreatedBy, &item.Kind, &item.FormatVersion, &item.FileBytes, &item.SHA256, &item.VerifiedAt, &item.Origin); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Repository) Backup(ctx context.Context, backupID string) (Backup, error) {
	var item Backup
	if !validBackupID(backupID) {
		return item, sql.ErrNoRows
	}
	err := r.Database.QueryRowContext(ctx, "SELECT id,created_at,created_by,kind,format_version,file_bytes,sha256,verified_at,origin FROM backups WHERE id=?", backupID).Scan(&item.ID, &item.CreatedAt, &item.CreatedBy, &item.Kind, &item.FormatVersion, &item.FileBytes, &item.SHA256, &item.VerifiedAt, &item.Origin)
	return item, err
}

type backupReader struct {
	ctx  context.Context
	file *os.File
}

func (r backupReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Read(data)
}

func (s *BackupService) openChecked(ctx context.Context, item Backup) (*os.File, error) {
	if !validBackupID(item.ID) {
		return nil, ErrBackupFile
	}
	path := filepath.Join(s.Directory, item.ID+".pcb")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != item.FileBytes || info.Size() > archive.MaxArchiveBytes {
		return nil, ErrBackupFile
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrBackupFile
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(backupReader{ctx, file}, archive.MaxArchiveBytes+1))
	if err != nil || n != item.FileBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		file.Close()
		return nil, errors.Join(ErrBackupFile, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (s *BackupService) Download(ctx context.Context, backupID, actor, ip string) (*os.File, Backup, error) {
	done, err := s.beginStore(ctx)
	if err != nil {
		return nil, Backup{}, err
	}
	defer done()
	item, err := s.Repository.Backup(ctx, backupID)
	if err != nil {
		return nil, item, err
	}
	file, err := s.openChecked(ctx, item)
	if err != nil {
		return nil, item, err
	}
	if err := s.Repository.Record(ctx, actor, "下载加密备份", item.ID, "成功", ip); err != nil {
		file.Close()
		return nil, item, err
	}
	return file, item, nil
}

func (s *BackupService) Verify(ctx context.Context, backupID, actor, ip, password string) (Backup, error) {
	if err := archive.ValidatePassword(password); err != nil {
		return Backup{}, err
	}
	done, err := s.beginStore(ctx)
	if err != nil {
		return Backup{}, err
	}
	defer done()
	item, err := s.Repository.Backup(ctx, backupID)
	if err != nil {
		return item, err
	}
	file, err := s.openChecked(ctx, item)
	if err != nil {
		return item, err
	}
	file.Close()
	result, err := archive.Inspect(ctx, s.Directory, filepath.Join(s.Directory, item.ID+".pcb"), password)
	if err != nil {
		return item, err
	}
	if result.Manifest.ID != item.ID || result.Manifest.CreatedBy != item.CreatedBy || result.SHA256 != item.SHA256 || result.Bytes != item.FileBytes {
		return item, ErrBackupFile
	}
	item.VerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE backups SET verified_at=? WHERE id=?", item.VerifiedAt, item.ID); err != nil {
		return item, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,'成功',?)", item.VerifiedAt, actor, "验证系统备份", item.ID, ip); err != nil {
		return item, err
	}
	return item, tx.Commit()
}

func (s *BackupService) Delete(ctx context.Context, backupID, actor, ip string) (resultErr error) {
	done, err := s.beginStore(ctx)
	if err != nil {
		return err
	}
	defer done()
	return s.deleteLocked(ctx, backupID, actor, ip, nil)
}

func (s *BackupService) deleteLocked(ctx context.Context, backupID, actor, ip string, guard *int64) (resultErr error) {
	item, err := s.Repository.Backup(ctx, backupID)
	if err != nil {
		return err
	}
	path := filepath.Join(s.Directory, item.ID+".pcb")
	trash := filepath.Join(s.Directory, ".trash-"+item.ID)
	info, fileErr := os.Lstat(path)
	if fileErr != nil && !os.IsNotExist(fileErr) || fileErr == nil && !info.Mode().IsRegular() {
		return ErrBackupFile
	}
	if _, err := os.Lstat(trash); !os.IsNotExist(err) {
		return ErrBackupFile
	}
	if err := s.Repository.prepareBackupFile(ctx, item, "delete"); err != nil {
		return err
	}
	if fileErr == nil {
		if err := os.Rename(path, trash); err != nil {
			return err
		}
	}
	committed := false
	defer func() {
		if !committed && fileErr == nil {
			resultErr = errors.Join(resultErr, os.Rename(trash, path))
		}
	}()
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if guard != nil {
		current, err := readBackupSchedule(ctx, tx)
		if err != nil {
			return err
		}
		if !current.Policy.Enabled || current.Revision != *guard {
			return ErrBackupScheduleConflict
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM backups WHERE id=?", item.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE backup_file_ops SET phase='committed' WHERE backup_id=? AND operation='delete'", item.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,'成功',?)", time.Now().UTC().Format(time.RFC3339Nano), actor, "删除系统备份", item.ID, ip); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	if s.Notify != nil {
		s.Notify()
	}
	if fileErr == nil {
		if err := os.Remove(trash); err != nil {
			return err
		}
	}
	return s.Repository.forgetBackupFile(ctx, item.ID)
}
