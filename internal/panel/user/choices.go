package user

import (
	"context"
	"net/http"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/httpapi"
)

type Choice struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

type Summary struct {
	Choice
	ExpiresAt    string   `json:"expiresAt"`
	TrafficLimit int64    `json:"trafficLimit"`
	UsedTraffic  int64    `json:"usedTraffic"`
	NodeGroupIDs []string `json:"nodeGroupIds"`
}

// Summaries is for quota cards and pickers that need grants, without live
// connection or device enforcement observations.
func (r *Repository) Summaries(ctx context.Context, page, size int) ([]Summary, int, error) {
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Close()
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,username,display_name,status,expires_at,traffic_limit,used_traffic FROM users ORDER BY created_at,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	items := []Summary{}
	for rows.Next() {
		var item Summary
		if err = rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status, &item.ExpiresAt, &item.TrafficLimit, &item.UsedTraffic); err != nil {
			rows.Close()
			return nil, 0, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].NodeGroupIDs = []string{}
		grants, readErr := tx.QueryContext(ctx, "SELECT group_id FROM user_node_group_grants WHERE user_id=? ORDER BY group_id", items[i].ID)
		if readErr != nil {
			return nil, 0, readErr
		}
		for grants.Next() {
			var group string
			if err = grants.Scan(&group); err != nil {
				grants.Close()
				return nil, 0, err
			}
			items[i].NodeGroupIDs = append(items[i].NodeGroupIDs, group)
		}
		err = grants.Err()
		grants.Close()
		if err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (h *Handler) summaries(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Repository.Summaries(r.Context(), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (r *Repository) Choices(ctx context.Context, keyword string, page, size int) ([]Choice, int, error) {
	search := "%" + keyword + "%"
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Close()
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE username LIKE ? OR display_name LIKE ?", search, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,username,display_name,status FROM users WHERE username LIKE ? OR display_name LIKE ? ORDER BY created_at,id LIMIT ? OFFSET ?", search, search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Choice{}
	for rows.Next() {
		var item Choice
		if err := rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (h *Handler) choices(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Repository.Choices(r.Context(), r.URL.Query().Get("keyword"), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
