package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

// maxNodeTags and maxNodeTagRunes bound the server-group tags of one server
// (v1.5.5 item 4).
const (
	maxNodeTags     = 10
	maxNodeTagRunes = 20
)

var ErrNodeTagsInvalid = errors.New("invalid server tags: at most 10, each 1 to 20 characters without control characters or commas")

// NormalizeTags trims each tag, rejects an invalid one and drops later
// duplicates compared case-insensitively, keeping the input order. Stored tags
// are always the output of this function.
func NormalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if n := utf8.RuneCountInString(tag); n < 1 || n > maxNodeTagRunes || !utf8.ValidString(tag) || strings.ContainsRune(tag, ',') || strings.IndexFunc(tag, unicode.IsControl) >= 0 {
			return nil, ErrNodeTagsInvalid
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	if len(out) > maxNodeTags {
		return nil, ErrNodeTagsInvalid
	}
	return out, nil
}

// DecodeTags reads a stored tags_json value; anything but a normalized list is
// reported as invalid.
func DecodeTags(encoded string) ([]string, error) {
	var tags []string
	if json.Unmarshal([]byte(encoded), &tags) != nil || tags == nil {
		return nil, ErrNodeTagsInvalid
	}
	normalized, err := NormalizeTags(tags)
	if err != nil || len(normalized) != len(tags) {
		return nil, ErrNodeTagsInvalid
	}
	for i := range tags {
		if tags[i] != normalized[i] {
			return nil, ErrNodeTagsInvalid
		}
	}
	return tags, nil
}

type Meta struct {
	Tags            []string `json:"tags"`
	PortRangeStart  *int     `json:"portRangeStart"`
	PortRangeEnd    *int     `json:"portRangeEnd"`
	BandwidthMbps   *int     `json:"bandwidthMbps"`
	Region          string   `json:"region"`
	Remark          string   `json:"remark"`
	RenewDate       string   `json:"renewDate"`
	RenewCycle      string   `json:"renewCycle"`
	RenewPrice      string   `json:"renewPrice"`
	RenewCurrency   string   `json:"renewCurrency"`
	RenewNoticeDays int      `json:"renewNoticeDays"`
}

func (m Meta) Validate() error {
	if (m.PortRangeStart == nil) != (m.PortRangeEnd == nil) {
		return errors.New("both port range bounds are required")
	}
	if m.PortRangeStart != nil && (*m.PortRangeStart < 1 || *m.PortRangeEnd > 65535 || *m.PortRangeStart > *m.PortRangeEnd) {
		return errors.New("invalid port range")
	}
	if m.BandwidthMbps != nil && (*m.BandwidthMbps < 1 || *m.BandwidthMbps > 100000) {
		return errors.New("invalid bandwidth")
	}
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
	m := Meta{RenewNoticeDays: 7, Tags: []string{}}
	var tags string
	err := r.Database.QueryRowContext(ctx, "SELECT region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,port_range_start,port_range_end,bandwidth_mbps,tags_json FROM node_meta WHERE node_id=?", id).Scan(&m.Region, &m.Remark, &m.RenewDate, &m.RenewCycle, &m.RenewPrice, &m.RenewCurrency, &m.RenewNoticeDays, &m.PortRangeStart, &m.PortRangeEnd, &m.BandwidthMbps, &tags)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err == nil {
		m.Tags, err = DecodeTags(tags)
	}
	return m, err == nil, err
}

// SaveMeta writes the metadata of one server inside tx. A nil tags keeps the
// stored tags, so a client that predates tags never clears them; importOnly
// writes only when the server has no metadata yet.
func SaveMeta(ctx context.Context, tx *sql.Tx, id string, m Meta, tags *[]string, importOnly bool) error {
	encoded := "[]"
	if tags != nil {
		data, err := json.Marshal(*tags)
		if err != nil {
			return err
		}
		encoded = string(data)
	}
	conflict := "DO UPDATE SET region=excluded.region,remark=excluded.remark,renew_date=excluded.renew_date,renew_cycle=excluded.renew_cycle,renew_price=excluded.renew_price,renew_currency=excluded.renew_currency,renew_notice_days=excluded.renew_notice_days,port_range_start=excluded.port_range_start,port_range_end=excluded.port_range_end,bandwidth_mbps=excluded.bandwidth_mbps"
	if tags != nil {
		conflict += ",tags_json=excluded.tags_json"
	}
	if importOnly {
		conflict = "DO NOTHING"
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO node_meta(node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,port_range_start,port_range_end,bandwidth_mbps,tags_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id) "+conflict, id, m.Region, m.Remark, m.RenewDate, m.RenewCycle, m.RenewPrice, m.RenewCurrency, m.RenewNoticeDays, m.PortRangeStart, m.PortRangeEnd, m.BandwidthMbps, encoded)
	return err
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
	// Tags shadows Meta.Tags so an omitted field (an older client) is told
	// apart from an empty list.
	var body struct {
		Meta
		Tags       *[]string `json:"tags"`
		ImportOnly bool      `json:"importOnly"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := body.Meta.Validate(); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.Tags != nil {
		tags, err := NormalizeTags(*body.Tags)
		if err != nil {
			h.fail(w, err)
			return
		}
		body.Tags = &tags
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
	err = SaveMeta(r.Context(), tx, r.PathValue("id"), body.Meta, body.Tags, body.ImportOnly)
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
