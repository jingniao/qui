package node

import (
	"context"
	"net/http"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/httpapi"
)

type MetaItem struct {
	NodeID  string `json:"nodeId"`
	Meta    Meta   `json:"meta"`
	Present bool   `json:"present"`
}

func (r *Repository) Metadata(ctx context.Context, deleted bool) ([]MetaItem, error) {
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return nil, err
	}
	defer tx.Close()
	scope := "n.deleted_at IS NULL"
	if deleted {
		scope = "n.deleted_at IS NOT NULL"
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.id,m.node_id IS NOT NULL,coalesce(m.region,''),coalesce(m.remark,''),coalesce(m.renew_date,''),coalesce(m.renew_cycle,''),coalesce(m.renew_price,''),coalesce(m.renew_currency,''),coalesce(m.renew_notice_days,7),m.port_range_start,m.port_range_end,m.bandwidth_mbps,coalesce(m.tags_json,'[]') FROM nodes n LEFT JOIN node_meta m ON m.node_id=n.id WHERE `+scope+` ORDER BY n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MetaItem{}
	for rows.Next() {
		var item MetaItem
		var tags string
		m := &item.Meta
		if err := rows.Scan(&item.NodeID, &item.Present, &m.Region, &m.Remark, &m.RenewDate, &m.RenewCycle, &m.RenewPrice, &m.RenewCurrency, &m.RenewNoticeDays, &m.PortRangeStart, &m.PortRangeEnd, &m.BandwidthMbps, &tags); err != nil {
			return nil, err
		}
		m.Tags, err = DecodeTags(tags)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) metadata(w http.ResponseWriter, r *http.Request) {
	items, err := h.Repository.Metadata(r.Context(), r.URL.Query().Get("deleted") == "1")
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
