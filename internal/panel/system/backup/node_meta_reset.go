package backup

import (
	"context"
	"database/sql"
	"proxycontrol/internal/panel/node"
	"time"
)

func validateNodeMetaReset(ctx context.Context, db *sql.DB) error {
	var metaApplied, resetApplied, present int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0117_node_meta.sql'").Scan(&metaApplied) != nil || db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0118_traffic_reset.sql'").Scan(&resetApplied) != nil || db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='node_meta'").Scan(&present) != nil || present != metaApplied {
		return ErrArchive
	}
	if metaApplied > 0 {
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
	}
	if resetApplied > 0 {
		rows, err := db.QueryContext(ctx, "SELECT traffic_reset_cycle,traffic_reset_anchor,traffic_reset_at FROM users")
		if err != nil {
			return ErrArchive
		}
		for rows.Next() {
			var cycle, anchor, last string
			if rows.Scan(&cycle, &anchor, &last) != nil || (cycle != "none" && cycle != "monthly" && cycle != "30days") || (cycle != "none" && anchor == "") {
				rows.Close()
				return ErrArchive
			}
			for _, stamp := range []string{anchor, last} {
				if stamp != "" {
					if _, err = time.Parse(time.RFC3339Nano, stamp); err != nil {
						rows.Close()
						return ErrArchive
					}
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrArchive
		}
	}
	return nil
}
