package access

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Traffic struct {
	Bucket        string `json:"bucket"`
	UploadBytes   int64  `json:"uploadBytes"`
	DownloadBytes int64  `json:"downloadBytes"`
}

func (r *Repository) Traffic(ctx context.Context, filter Filter, granularity string, page, size int) ([]Traffic, int, error) {
	table, from, to := "traffic_hourly", timestamp(filter.From), timestamp(filter.To)
	if granularity == "daily" {
		table = "traffic_daily"
		from = filter.From.UTC().Format("2006-01-02")
		to = filter.To.UTC().Format("2006-01-02")
		if !filter.To.UTC().Equal(filter.To.UTC().Truncate(24 * time.Hour)) {
			to = filter.To.UTC().AddDate(0, 0, 1).Format("2006-01-02")
		}
	} else if granularity != "hourly" {
		return nil, 0, errors.New("invalid traffic granularity")
	} else {
		from = timestamp(filter.From.Truncate(time.Hour))
		end := filter.To.Truncate(time.Hour)
		if !end.Equal(filter.To) {
			end = end.Add(time.Hour)
		}
		to = timestamp(end)
	}
	parts := []string{"bucket>=?", "bucket<?"}
	args := []any{from, to}
	for _, entry := range []struct{ key, value string }{{"node_id", filter.NodeID}, {"user_id", filter.UserID}, {"device_id", filter.DeviceID}} {
		if entry.value != "" {
			parts = append(parts, entry.key+"=?")
			args = append(args, entry.value)
		}
	}
	where := " WHERE " + strings.Join(parts, " AND ")
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
