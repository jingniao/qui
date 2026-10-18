package access

import (
	"context"
	"errors"
	"strings"
	"time"
)

type AccountSummary struct {
	UploadBytes   int64 `json:"uploadBytes"`
	DownloadBytes int64 `json:"downloadBytes"`
	Observations  int64 `json:"observations"`
}

func accountWhere(filter Filter, from, to string, column string) (string, []any) {
	parts := []string{column + ">=?", column + "<?"}
	args := []any{from, to}
	for _, entry := range []struct{ key, value string }{{"node_id", filter.NodeID}, {"user_id", filter.UserID}, {"device_id", filter.DeviceID}} {
		if entry.value != "" {
			parts = append(parts, entry.key+"=?")
			args = append(args, entry.value)
		}
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}
func (r *Repository) AccountSummary(ctx context.Context, filter Filter) (AccountSummary, error) {
	var result AccountSummary
	where, args := accountWhere(filter, filter.From.UTC().Format("2006-01-02T15:04:05.000000000Z"), filter.To.UTC().Format("2006-01-02T15:04:05.000000000Z"), "observed_at")
	err := r.Database.QueryRowContext(ctx, "SELECT coalesce(sum(upload_bytes),0),coalesce(sum(download_bytes),0),count(*) FROM account_traffic_samples"+where, args...).Scan(&result.UploadBytes, &result.DownloadBytes, &result.Observations)
	return result, err
}
func (r *Repository) AccountTraffic(ctx context.Context, filter Filter, granularity string, page, size int) ([]Traffic, int, error) {
	table, from, to := "account_traffic_hourly", timestamp(filter.From.Truncate(time.Hour)), timestamp(filter.To.Truncate(time.Hour))
	if granularity == "daily" {
		table = "account_traffic_daily"
		from = filter.From.UTC().Format("2006-01-02")
		to = filter.To.UTC().Format("2006-01-02")
		if !filter.To.Equal(filter.To.Truncate(24 * time.Hour)) {
			to = filter.To.UTC().AddDate(0, 0, 1).Format("2006-01-02")
		}
	} else if granularity == "hourly" {
		if !filter.To.Equal(filter.To.Truncate(time.Hour)) {
			to = timestamp(filter.To.Truncate(time.Hour).Add(time.Hour))
		}
	} else {
		return nil, 0, errors.New("invalid account granularity")
	}
	where, args := accountWhere(filter, from, to, "bucket")
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT bucket FROM "+table+where+" GROUP BY bucket)", args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, "SELECT bucket,sum(upload_bytes),sum(download_bytes) FROM "+table+where+" GROUP BY bucket ORDER BY bucket DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Traffic{}
	for rows.Next() {
		var item Traffic
		if err := rows.Scan(&item.Bucket, &item.UploadBytes, &item.DownloadBytes); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, count, rows.Err()
}
