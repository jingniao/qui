package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrBaseInvalid = errors.New("invalid platform name or explicit IANA timezone")
var ErrBaseConflict = errors.New("base settings changed; refresh before saving")

type BaseSettings struct {
	PlatformName string `json:"platformName"`
	Timezone     string `json:"timezone"`
	Revision     int64  `json:"revision"`
	UpdatedAt    string `json:"updatedAt"`
	UpdatedBy    string `json:"updatedBy"`
}
type BaseInput struct {
	PlatformName     string `json:"platformName"`
	Timezone         string `json:"timezone"`
	ExpectedRevision int64  `json:"expectedRevision"`
}
type baseMetadata struct {
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

func normalizeBase(name, zone string) (string, string, error) {
	name = strings.TrimSpace(name)
	zone = strings.TrimSpace(zone)
	if !utf8.ValidString(name) || len(name) < 1 || len(name) > 120 || zone == "" || zone == "Local" || len(zone) > 128 {
		return name, zone, ErrBaseInvalid
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return name, zone, ErrBaseInvalid
		}
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return name, zone, ErrBaseInvalid
	}
	return name, zone, nil
}
func readBase(ctx context.Context, tx *sql.Tx) (BaseSettings, error) {
	settings := BaseSettings{PlatformName: "Proxy Control", Timezone: "Asia/Shanghai"}
	rows, err := tx.QueryContext(ctx, "SELECT key,value_json FROM system_settings WHERE key IN ('platform_name','timezone','base_settings_meta')")
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, encoded string
		if err := rows.Scan(&key, &encoded); err != nil {
			return settings, err
		}
		switch key {
		case "platform_name":
			if err := json.Unmarshal([]byte(encoded), &settings.PlatformName); err != nil {
				return settings, ErrBaseInvalid
			}
		case "timezone":
			if err := json.Unmarshal([]byte(encoded), &settings.Timezone); err != nil {
				return settings, ErrBaseInvalid
			}
		case "base_settings_meta":
			var metadata baseMetadata
			if err := json.Unmarshal([]byte(encoded), &metadata); err != nil || metadata.Revision < 1 {
				return settings, ErrBaseInvalid
			}
			if _, err := time.Parse(time.RFC3339Nano, metadata.UpdatedAt); err != nil {
				return settings, ErrBaseInvalid
			}
			settings.Revision, settings.UpdatedAt, settings.UpdatedBy = metadata.Revision, metadata.UpdatedAt, metadata.UpdatedBy
		}
	}
	if err := rows.Err(); err != nil {
		return settings, err
	}
	name, zone, err := normalizeBase(settings.PlatformName, settings.Timezone)
	if err != nil || name != settings.PlatformName || zone != settings.Timezone {
		return settings, ErrBaseInvalid
	}
	return settings, nil
}
func (r *Repository) ReadBase(ctx context.Context) (BaseSettings, error) {
	tx, err := r.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BaseSettings{}, err
	}
	defer tx.Rollback()
	settings, err := readBase(ctx, tx)
	if err != nil {
		return settings, err
	}
	return settings, tx.Commit()
}
func (r *Repository) SaveBase(ctx context.Context, input BaseInput, actor, ip string) (BaseSettings, bool, error) {
	name, zone, err := normalizeBase(input.PlatformName, input.Timezone)
	if err != nil || input.ExpectedRevision < 0 {
		return BaseSettings{}, false, ErrBaseInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return BaseSettings{}, false, err
	}
	defer tx.Rollback()
	current, err := readBase(ctx, tx)
	if err != nil {
		return current, false, err
	}
	if name == current.PlatformName && zone == current.Timezone {
		return current, false, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, false, ErrBaseConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := BaseSettings{PlatformName: name, Timezone: zone, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
	for key, value := range map[string]any{"platform_name": name, "timezone": zone, "base_settings_meta": baseMetadata{Revision: next.Revision, UpdatedAt: stamp, UpdatedBy: actor}} {
		data, err := json.Marshal(value)
		if err != nil {
			return next, false, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", key, string(data)); err != nil {
			return next, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改平台基础设置','platform_name/timezone','成功',?)", stamp, actor, ip); err != nil {
		return next, false, err
	}
	if err := tx.Commit(); err != nil {
		return next, false, err
	}
	return next, true, nil
}
