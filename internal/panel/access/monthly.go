package access

import (
	"context"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	"time"
)

type NodeMonth struct {
	NodeID        string `json:"nodeId"`
	UploadBytes   int64  `json:"uploadBytes"`
	DownloadBytes int64  `json:"downloadBytes"`
}

// MonthlyNodes sums timestamped account deltas, never domain estimates or
// cumulative heartbeat counters. Both bounds are local month boundaries.
func (r *Repository) MonthlyNodes(ctx context.Context, now time.Time, zone string) ([]NodeMonth, error) {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, err
	}
	local := now.In(location)
	from := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	rows, err := r.Database.QueryContext(ctx, `SELECT n.id,coalesce(sum(t.upload_bytes),0),coalesce(sum(t.download_bytes),0)
 FROM nodes n LEFT JOIN node_traffic_samples t ON t.node_id=n.id AND t.observed_at>=? AND t.observed_at<?
 WHERE n.deleted_at IS NULL GROUP BY n.id ORDER BY n.id`, from.UTC().Format("2006-01-02T15:04:05.000000000Z"), from.AddDate(0, 1, 0).UTC().Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NodeMonth{}
	for rows.Next() {
		var item NodeMonth
		if err = rows.Scan(&item.NodeID, &item.UploadBytes, &item.DownloadBytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (h *Handler) monthlyNodes(w http.ResponseWriter, r *http.Request) {
	_, zone, err := h.BaseSettings.ReadSetup(r.Context())
	if err != nil {
		h.fail(w)
		return
	}
	items, err := h.Repository.MonthlyNodes(r.Context(), time.Now(), zone)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "timezone": zone, "scope": "retained_account_samples"})
}
