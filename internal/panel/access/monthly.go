package access

import (
	"context"
	"errors"
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
	totals, err := ReadNodeTraffic(ctx, r.Database, from, from.AddDate(0, 1, 0), "", false)
	if err != nil {
		return nil, err
	}
	byNode := map[string]NodeTrafficTotal{}
	for _, item := range totals {
		byNode[item.NodeID] = item
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM nodes WHERE deleted_at IS NULL ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NodeMonth{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		v := byNode[id]
		items = append(items, NodeMonth{NodeID: id, UploadBytes: v.Upload, DownloadBytes: v.Download})
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
		if errors.Is(err, ErrTrafficResolution) {
			httpapi.Error(w, 422, "traffic_history_resolution", err.Error())
			return
		}
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "timezone": zone, "scope": "retained_account_samples"})
}
