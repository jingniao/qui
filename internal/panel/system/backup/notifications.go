package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/internal/panel/notification"
	"proxycontrol/internal/platform/vault"
)

func validateNotifications(ctx context.Context, database *sql.DB, vault *vault.Vault) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0139_notifications.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	// 1. validate rules
	rows, err := database.QueryContext(ctx, "SELECT kind, enabled, threshold, duration_minutes, cooldown_minutes FROM notification_rules")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	ruleKinds := map[string]bool{
		"node_offline": true, "cpu": true, "memory": true, "disk": true,
		"public_ip": true, "machine_traffic": true, "system_alert": true,
	}
	for rows.Next() {
		var kind string
		var enabled int
		var threshold float64
		var duration, cooldown int
		if err := rows.Scan(&kind, &enabled, &threshold, &duration, &cooldown); err != nil {
			return ErrArchive
		}
		if !ruleKinds[kind] || (enabled != 0 && enabled != 1) {
			return ErrArchive
		}
		if cooldown < 0 || cooldown > 1440 || duration < 0 || duration > 1440 {
			return ErrArchive
		}
		switch kind {
		case "cpu", "memory", "disk", "machine_traffic":
			if threshold < 1 || threshold > 100 {
				return ErrArchive
			}
		}
	}
	if err := rows.Err(); err != nil {
		return ErrArchive
	}
	rows.Close()

	// 2. validate channels
	chRows, err := database.QueryContext(ctx, "SELECT id, kind, enabled, encrypted_config FROM notification_channels")
	if err != nil {
		return ErrArchive
	}
	defer chRows.Close()
	for chRows.Next() {
		var id, kind string
		var enabled int
		var encrypted []byte
		if err := chRows.Scan(&id, &kind, &enabled, &encrypted); err != nil {
			return ErrArchive
		}
		if kind != "telegram" && kind != "webhook" {
			return ErrArchive
		}
		if enabled != 0 && enabled != 1 {
			return ErrArchive
		}
		if vault != nil && len(encrypted) > 0 {
			decrypted, err := vault.Open(encrypted)
			if err != nil {
				return ErrArchive
			}
			var cfg notification.ChannelConfig
			decoder := json.NewDecoder(bytes.NewReader(decrypted))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&cfg); err != nil || cfg.Validate(kind) != nil || !json.Valid(decrypted) {
				return ErrArchive
			}
		}
	}
	if err := chRows.Err(); err != nil {
		return ErrArchive
	}
	chRows.Close()

	// 3. validate notifications
	nRows, err := database.QueryContext(ctx, "SELECT severity FROM notifications")
	if err != nil {
		return ErrArchive
	}
	defer nRows.Close()
	for nRows.Next() {
		var severity string
		if err := nRows.Scan(&severity); err != nil {
			return ErrArchive
		}
		if severity != "info" && severity != "warning" && severity != "critical" && severity != "resolved" {
			return ErrArchive
		}
	}
	if err := nRows.Err(); err != nil {
		return ErrArchive
	}
	return nil
}
