package backup

import (
	"context"
	"database/sql"
	"proxycontrol/internal/panel/node"
	"time"
)

// validateMachineTraffic enforces the 0126 tables and their stored values. The
// daily rows and cursors are durable traffic data, not seven-day telemetry, so
// they stay in the archive.
func validateMachineTraffic(ctx context.Context, db *sql.DB) error {
	var applied, present int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0126_node_machine_traffic.sql'").Scan(&applied) != nil ||
		db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('node_machine_traffic_settings','node_machine_traffic_daily','node_machine_traffic_cursors')").Scan(&present) != nil ||
		present != applied*3 {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	// Every expected column is read even when the table is empty: the column
	// layout is part of the schema this build wrote.
	rows, err := db.QueryContext(ctx, "SELECT node_id,method,interface_name,allowance_bytes,reset_day FROM node_machine_traffic_settings")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var id string
		var settings node.MachineTrafficSettings
		if rows.Scan(&id, &settings.Method, &settings.InterfaceName, &settings.AllowanceBytes, &settings.ResetDay) != nil || id == "" || node.ValidateMachineTrafficSettings(settings) != nil {
			rows.Close()
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrArchive
	}
	rows.Close()
	rows, err = db.QueryContext(ctx, "SELECT node_id,interface_name,local_date,rx_bytes,tx_bytes FROM node_machine_traffic_daily")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var id, iface, day string
		var rx, tx any
		if rows.Scan(&id, &iface, &day, &rx, &tx) != nil || id == "" || !node.ValidMachineTrafficInterface(iface, false) || !node.ValidMachineDay(day) || !storedAmount(rx) || !storedAmount(tx) {
			rows.Close()
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrArchive
	}
	rows.Close()
	rows, err = db.QueryContext(ctx, "SELECT node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at FROM node_machine_traffic_cursors")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var id, iface, boot, observed string
		var rx, tx any
		if rows.Scan(&id, &iface, &boot, &rx, &tx, &observed) != nil || id == "" || !node.ValidMachineTrafficInterface(iface, false) || !node.ValidBootID(boot) || !storedAmount(rx) || !storedAmount(tx) || !utcTimestamp(observed) {
			return ErrArchive
		}
	}
	return rows.Err()
}

// storedAmount accepts only a nonnegative SQLite integer; a REAL would have lost
// the exact counter.
func storedAmount(value any) bool {
	amount, ok := value.(int64)
	return ok && amount >= 0
}

// utcTimestamp accepts the fixed-width UTC text the panel stores.
func utcTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC
}
