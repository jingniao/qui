package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/vault"
	"strconv"
	"time"
)

var ErrBackupScheduleInvalid = errors.New("invalid or unavailable backup schedule")
var ErrBackupScheduleConflict = errors.New("backup schedule changed; refresh before saving")

type BackupPolicy struct {
	Enabled     bool   `json:"enabled"`
	Time        string `json:"time"`
	DailyKeep   int    `json:"dailyKeep"`
	WeeklyKeep  int    `json:"weeklyKeep"`
	MonthlyKeep int    `json:"monthlyKeep"`
}
type BackupSchedule struct {
	Policy             BackupPolicy `json:"policy"`
	Revision           int64        `json:"revision"`
	UpdatedAt          string       `json:"updatedAt"`
	UpdatedBy          string       `json:"updatedBy"`
	PasswordConfigured bool         `json:"passwordConfigured"`
	Timezone           string       `json:"timezone"`
}
type BackupScheduleInput struct {
	Policy           BackupPolicy `json:"policy"`
	ExpectedRevision int64        `json:"expectedRevision"`
	Password         string       `json:"password"`
}
type storedBackupSchedule struct {
	Policy    BackupPolicy `json:"policy"`
	Revision  int64        `json:"revision"`
	UpdatedAt string       `json:"updatedAt"`
	UpdatedBy string       `json:"updatedBy"`
	Password  []byte       `json:"encrypted_password"`
}

var defaultBackupPolicy = BackupPolicy{Time: "03:00", DailyKeep: 7, WeeklyKeep: 4, MonthlyKeep: 3}

func validateBackupPolicy(policy BackupPolicy) error {
	if len(policy.Time) != 5 || policy.Time[2] != ':' {
		return ErrBackupScheduleInvalid
	}
	hour, errH := strconv.Atoi(policy.Time[:2])
	minute, errM := strconv.Atoi(policy.Time[3:])
	if errH != nil || errM != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 || policy.Time != fmtClock(hour, minute) {
		return ErrBackupScheduleInvalid
	}
	for _, keep := range []int{policy.DailyKeep, policy.WeeklyKeep, policy.MonthlyKeep} {
		if keep < 1 || keep > 365 {
			return ErrBackupScheduleInvalid
		}
	}
	return nil
}
func fmtClock(hour, minute int) string {
	return string([]byte{'0' + byte(hour/10), '0' + byte(hour%10), ':', '0' + byte(minute/10), '0' + byte(minute%10)})
}

func readBackupSchedule(ctx context.Context, tx *sql.Tx) (storedBackupSchedule, error) {
	stored := storedBackupSchedule{Policy: defaultBackupPolicy}
	var data string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='backup_schedule'").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, nil
	}
	if err != nil {
		return stored, err
	}
	if len(data) > 8192 {
		return stored, ErrBackupScheduleInvalid
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil || decoder.Decode(&struct{}{}) != io.EOF || stored.Revision < 1 || len(stored.Password) > 1024 {
		return stored, ErrBackupScheduleInvalid
	}
	if err := validateBackupPolicy(stored.Policy); err != nil {
		return stored, err
	}
	if _, err := time.Parse(time.RFC3339Nano, stored.UpdatedAt); err != nil {
		return stored, ErrBackupScheduleInvalid
	}
	if stored.UpdatedBy == "" || stored.Policy.Enabled && len(stored.Password) == 0 {
		return stored, ErrBackupScheduleInvalid
	}
	return stored, nil
}

func publicBackupSchedule(stored storedBackupSchedule, zone string) BackupSchedule {
	return BackupSchedule{Policy: stored.Policy, Revision: stored.Revision, UpdatedAt: stored.UpdatedAt, UpdatedBy: stored.UpdatedBy, PasswordConfigured: len(stored.Password) > 0, Timezone: zone}
}
func (r *Repository) BackupSchedule(ctx context.Context) (BackupSchedule, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BackupSchedule{}, err
	}
	defer tx.Rollback()
	stored, err := readBackupSchedule(ctx, tx)
	if err != nil {
		return BackupSchedule{}, err
	}
	base, err := readBase(ctx, tx)
	if err != nil {
		return BackupSchedule{}, err
	}
	return publicBackupSchedule(stored, base.Timezone), tx.Commit()
}

func backupPassword(key *vault.Vault, stored storedBackupSchedule) (string, error) {
	if key == nil || len(stored.Password) == 0 {
		return "", ErrBackupScheduleInvalid
	}
	data, err := key.Open(stored.Password)
	if err != nil {
		return "", ErrBackupScheduleInvalid
	}
	defer clear(data)
	password := string(data)
	if err := archive.ValidatePassword(password); err != nil {
		return "", ErrBackupScheduleInvalid
	}
	return password, nil
}

func (s *BackupService) SaveSchedule(ctx context.Context, input BackupScheduleInput, actor, ip string) (BackupSchedule, bool, error) {
	if err := validateBackupPolicy(input.Policy); err != nil || input.ExpectedRevision < 0 || actor == "" {
		return BackupSchedule{}, false, ErrBackupScheduleInvalid
	}
	if input.Password != "" {
		if err := archive.ValidatePassword(input.Password); err != nil {
			return BackupSchedule{}, false, err
		}
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return BackupSchedule{}, false, err
	}
	defer tx.Rollback()
	current, err := readBackupSchedule(ctx, tx)
	if err != nil {
		return BackupSchedule{}, false, err
	}
	base, err := readBase(ctx, tx)
	if err != nil {
		return BackupSchedule{}, false, err
	}
	passwordChanged := false
	if input.Password != "" {
		oldPassword, err := backupPassword(s.Source.ConfigurationVault, current)
		passwordChanged = err != nil || input.Password != oldPassword
	}
	if current.Policy == input.Policy && !passwordChanged {
		return publicBackupSchedule(current, base.Timezone), false, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return BackupSchedule{}, false, ErrBackupScheduleConflict
	}
	next := current
	if passwordChanged {
		if s.Source.ConfigurationVault == nil {
			return BackupSchedule{}, false, ErrBackupScheduleInvalid
		}
		sealed, err := s.Source.ConfigurationVault.Seal([]byte(input.Password))
		if err != nil {
			return BackupSchedule{}, false, err
		}
		next.Password = sealed
	}
	if input.Policy.Enabled {
		if _, err := backupPassword(s.Source.ConfigurationVault, next); err != nil {
			return BackupSchedule{}, false, err
		}
	}
	next.Policy = input.Policy
	next.Revision = current.Revision + 1
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	next.UpdatedBy = actor
	data, err := json.Marshal(next)
	if err != nil {
		return BackupSchedule{}, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('backup_schedule',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data)); err != nil {
		return BackupSchedule{}, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改自动备份策略','backup_schedule','成功',?)", next.UpdatedAt, actor, ip); err != nil {
		return BackupSchedule{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return BackupSchedule{}, false, err
	}
	return publicBackupSchedule(next, base.Timezone), true, nil
}
