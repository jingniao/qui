package node

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type Meta struct {
	Region          string `json:"region"`
	Remark          string `json:"remark"`
	RenewDate       string `json:"renewDate"`
	RenewCycle      string `json:"renewCycle"`
	RenewPrice      string `json:"renewPrice"`
	RenewCurrency   string `json:"renewCurrency"`
	RenewNoticeDays int    `json:"renewNoticeDays"`
}

func (m Meta) Validate() error {
	if len(m.Region) > 120 || len(m.Remark) > 1024 || len(m.RenewCycle) > 40 || len(m.RenewCurrency) > 8 || len(m.RenewPrice) > 32 || m.RenewNoticeDays < 0 || m.RenewNoticeDays > 365 {
		return errors.New("invalid node metadata")
	}
	if m.RenewDate != "" {
		if _, err := time.Parse("2006-01-02", m.RenewDate); err != nil {
			return errors.New("invalid renewal date")
		}
	}
	if m.RenewPrice != "" {
		n, err := strconv.ParseFloat(m.RenewPrice, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
			return errors.New("invalid renewal price")
		}
	}
	return nil
}
func (r *Repository) Meta(ctx context.Context, id string) (Meta, bool, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return Meta{}, false, err
	}
	m := Meta{RenewNoticeDays: 7}
	err := r.Database.QueryRowContext(ctx, "SELECT region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days FROM node_meta WHERE node_id=?", id).Scan(&m.Region, &m.Remark, &m.RenewDate, &m.RenewCycle, &m.RenewPrice, &m.RenewCurrency, &m.RenewNoticeDays)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	return m, err == nil, err
}
func (h *Handler) getMeta(w http.ResponseWriter, r *http.Request) {
	m, present, err := h.Repository.Meta(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"meta": m, "present": present})
}
func (h *Handler) putMeta(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Meta
		ImportOnly bool `json:"importOnly"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := body.Meta.Validate(); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if _, err := h.Repository.Get(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	tx, err := h.Repository.Database.BeginTx(r.Context(), nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	conflict := "DO UPDATE SET region=excluded.region,remark=excluded.remark,renew_date=excluded.renew_date,renew_cycle=excluded.renew_cycle,renew_price=excluded.renew_price,renew_currency=excluded.renew_currency,renew_notice_days=excluded.renew_notice_days"
	if body.ImportOnly {
		conflict = "DO NOTHING"
	}
	m := body.Meta
	_, err = tx.ExecContext(r.Context(), "INSERT INTO node_meta(node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(node_id) "+conflict, r.PathValue("id"), m.Region, m.Remark, m.RenewDate, m.RenewCycle, m.RenewPrice, m.RenewCurrency, m.RenewNoticeDays)
	if err == nil {
		err = h.Audit.RecordTx(r.Context(), tx, auth.Actor(r.Context()).Username, "节点元数据", r.PathValue("id"), "成功", r.RemoteAddr)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	h.getMeta(w, r)
}

// RenewalAlerts claims a local calendar day before publishing, preventing a
// panel restart (or simultaneous scheduler calls) from duplicating an alert.
func (r *Repository) RenewalAlerts(ctx context.Context, now time.Time, location *time.Location) ([]string, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	day := now.In(location).Format("2006-01-02")
	rows, err := tx.QueryContext(ctx, "SELECT m.node_id,m.renew_date,m.renew_notice_days FROM node_meta m JOIN nodes n ON n.id=m.node_id WHERE n.deleted_at IS NULL AND m.renew_date<>'' AND m.last_alert_day<?", day)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id, date string
		var days int
		if err = rows.Scan(&id, &date, &days); err != nil {
			rows.Close()
			return nil, err
		}
		renew, parseErr := time.ParseInLocation("2006-01-02", date, location)
		if parseErr != nil {
			rows.Close()
			return nil, parseErr
		}
		if day >= renew.AddDate(0, 0, -days).Format("2006-01-02") {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, "UPDATE node_meta SET last_alert_day=? WHERE node_id=?", day, id); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit()
}
