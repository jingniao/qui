package access

import (
	"context"
	"database/sql"
	contract "proxycontrol/pkg/contracts/access"
	"time"
)

type Retention = contract.Retention

var DefaultRetention = contract.DefaultRetention

type RetentionSource interface {
	AccessRetentionInTx(context.Context, *sql.Tx) (contract.Retention, error)
}

// StorageThresholdSource supplies the Owner-configured advisory byte
// thresholds. It is injected by the central system repository so the access
// storage observation can classify capacity without owning settings. A zero
// pair means "not configured yet", which must stay "unknown" until measured
// growth has been used to choose values.
type StorageThresholdSource interface {
	StorageThresholdsInTx(context.Context, *sql.Tx) (warningBytes, criticalBytes int64, err error)
}

func (r *Repository) retentionInTx(ctx context.Context, tx *sql.Tx) (Retention, error) {
	retention := DefaultRetention
	if r.Settings != nil {
		var err error
		retention, err = r.Settings.AccessRetentionInTx(ctx, tx)
		if err != nil {
			return retention, err
		}
	}
	return retention, retention.Validate()
}
func (r *Repository) PruneConfigured(ctx context.Context, now time.Time) error {
	for {
		pending, err := r.AdvanceRetention(ctx, now)
		if err != nil || !pending {
			return err
		}
	}
}
func pruneInTx(ctx context.Context, tx *sql.Tx, now time.Time, retention Retention) error {
	now = now.UTC()
	if err := retention.Validate(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM access_rollups WHERE (allowed=1 AND bucket<?) OR (allowed=0 AND bucket<?)", timestamp(now.AddDate(0, 0, -retention.AllowedDays)), timestamp(now.AddDate(0, 0, -retention.BlockedDays))); err != nil {
		return err
	}
	for _, table := range []string{"traffic_hourly", "account_traffic_hourly"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE bucket<?", timestamp(now.AddDate(0, 0, -retention.HourlyDays))); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM account_traffic_samples WHERE observed_at<?", now.AddDate(0, 0, -retention.HourlyDays).UTC().Format("2006-01-02T15:04:05.000000000Z"))
	return err
}
