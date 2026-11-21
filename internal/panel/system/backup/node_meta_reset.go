package backup

import (
	"context"
	"database/sql"
	"proxycontrol/internal/panel/node"
	"time"
)

func validateNodeMetaReset(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,last_alert_day FROM node_meta")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var m node.Meta
		var day string
		if rows.Scan(&m.Region, &m.Remark, &m.RenewDate, &m.RenewCycle, &m.RenewPrice, &m.RenewCurrency, &m.RenewNoticeDays, &day) != nil || m.Validate() != nil {
			rows.Close()
			return ErrArchive
		}
		if day != "" {
			if _, err = time.Parse("2006-01-02", day); err != nil {
				rows.Close()
				return ErrArchive
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	rows, err = db.QueryContext(ctx, "SELECT traffic_reset_cycle,traffic_reset_anchor,traffic_reset_at FROM users")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var cycle, anchor, last string
		if rows.Scan(&cycle, &anchor, &last) != nil || (cycle != "none" && cycle != "monthly" && cycle != "30days") || (cycle != "none" && anchor == "") {
			return ErrArchive
		}
		for _, stamp := range []string{anchor, last} {
			if stamp != "" {
				if _, err = time.Parse(time.RFC3339Nano, stamp); err != nil {
					return ErrArchive
				}
			}
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

// validateNodeAutoRenew skips archives that predate the auto renewal column.
// An enabled row must have the date and recurring cycle the scheduler needs.
func validateNodeAutoRenew(ctx context.Context, database *sql.DB) error {
	var applied bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", "0160_node_meta_auto_renew.sql").Scan(&applied); err != nil {
		return ErrArchive
	}
	if !applied {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT renew_date,renew_cycle,renew_notice_days FROM node_meta WHERE auto_renew=1")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		m := node.Meta{AutoRenew: true}
		if rows.Scan(&m.RenewDate, &m.RenewCycle, &m.RenewNoticeDays) != nil || m.Validate() != nil {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}
