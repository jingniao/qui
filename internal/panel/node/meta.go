package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	// AutoRenew moves a passed renewal date forward by whole cycles (see AutoRenew).
	AutoRenew bool `json:"autoRenew"`
}

// renewalCycleMonths maps the recurring cycles the editor offers to months.
// "一次性" and free text have no entry, so they cannot renew automatically.
var renewalCycleMonths = map[string]int{"/月": 1, "/季": 3, "/半年": 6, "/年": 12, "/2年": 24, "/3年": 36}

var errAutoRenewIneligible = errors.New("auto renewal requires a renewal date and a recurring cycle")

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
	if m.AutoRenew && (m.RenewDate == "" || renewalCycleMonths[m.RenewCycle] == 0) {
		return errAutoRenewIneligible
	}
	return nil
}
func (r *Repository) Meta(ctx context.Context, id string) (Meta, bool, error) {
	var exists int
	if err := r.Database.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id=?", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return Meta{}, false, err
	}
	m := Meta{RenewNoticeDays: 7, Tags: []string{}}
	var tags string
	err := r.Database.QueryRowContext(ctx, "SELECT region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,port_range_start,port_range_end,bandwidth_mbps,tags_json,auto_renew FROM node_meta WHERE node_id=?", id).Scan(&m.Region, &m.Remark, &m.RenewDate, &m.RenewCycle, &m.RenewPrice, &m.RenewCurrency, &m.RenewNoticeDays, &m.PortRangeStart, &m.PortRangeEnd, &m.BandwidthMbps, &tags, &m.AutoRenew)
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
	conflict := "DO UPDATE SET region=excluded.region,remark=excluded.remark,renew_date=excluded.renew_date,renew_cycle=excluded.renew_cycle,renew_price=excluded.renew_price,renew_currency=excluded.renew_currency,renew_notice_days=excluded.renew_notice_days,port_range_start=excluded.port_range_start,port_range_end=excluded.port_range_end,bandwidth_mbps=excluded.bandwidth_mbps,auto_renew=excluded.auto_renew"
	if tags != nil {
		conflict += ",tags_json=excluded.tags_json"
	}
	if importOnly {
		conflict = "DO NOTHING"
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO node_meta(node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,port_range_start,port_range_end,bandwidth_mbps,tags_json,auto_renew) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id) "+conflict, id, m.Region, m.Remark, m.RenewDate, m.RenewCycle, m.RenewPrice, m.RenewCurrency, m.RenewNoticeDays, m.PortRangeStart, m.PortRangeEnd, m.BandwidthMbps, encoded, m.AutoRenew)
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
	// Tags and AutoRenew shadow the Meta fields so an omitted field (an older
	// client) is told apart from an empty list or false.
	var body struct {
		Meta
		Tags       *[]string `json:"tags"`
		AutoRenew  *bool     `json:"autoRenew"`
		ImportOnly bool      `json:"importOnly"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.AutoRenew != nil {
		body.Meta.AutoRenew = *body.AutoRenew
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
	if body.AutoRenew == nil {
		// Keep the stored choice; it must still fit the new date and cycle.
		err = tx.QueryRowContext(r.Context(), "SELECT auto_renew FROM node_meta WHERE node_id=?", r.PathValue("id")).Scan(&body.Meta.AutoRenew)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			h.fail(w, err)
			return
		}
		if err = body.Meta.Validate(); err != nil {
			httpapi.Error(w, 400, "invalid_request", err.Error())
			return
		}
	}
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
	h.Runtime.afterCommit(r.PathValue("id"))
	h.getMeta(w, r)
}

type RenewalAlert struct {
	NodeID, Name, RenewDate string
	DaysLeft                int
	AutoRenew               bool
}

// RenewalAlerts claims a local calendar day before publishing, preventing a
// panel restart (or simultaneous scheduler calls) from duplicating an alert.
func (r *Repository) RenewalAlerts(ctx context.Context, now time.Time, location *time.Location) ([]RenewalAlert, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	day := now.In(location).Format("2006-01-02")
	rows, err := tx.QueryContext(ctx, "SELECT m.node_id,n.name,m.renew_date,m.renew_notice_days,m.auto_renew FROM node_meta m JOIN nodes n ON n.id=m.node_id WHERE n.deleted_at IS NULL AND m.renew_date<>'' AND m.last_alert_day<?", day)
	if err != nil {
		return nil, err
	}
	alerts := []RenewalAlert{}
	for rows.Next() {
		var item RenewalAlert
		var days int
		if err = rows.Scan(&item.NodeID, &item.Name, &item.RenewDate, &days, &item.AutoRenew); err != nil {
			rows.Close()
			return nil, err
		}
		renew, parseErr := time.ParseInLocation("2006-01-02", item.RenewDate, location)
		if parseErr != nil {
			rows.Close()
			return nil, parseErr
		}
		if day >= renew.AddDate(0, 0, -days).Format("2006-01-02") {
			// Count calendar dates, including across DST changes in panel timezone.
			y, m, d := now.In(location).Date()
			ry, rm, rd := renew.Date()
			item.DaysLeft = int(time.Date(ry, rm, rd, 0, 0, 0, 0, time.UTC).Unix()/86400 - time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()/86400)
			alerts = append(alerts, item)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, item := range alerts {
		if _, err = tx.ExecContext(ctx, "UPDATE node_meta SET last_alert_day=? WHERE node_id=?", day, item.NodeID); err != nil {
			return nil, err
		}
	}
	return alerts, tx.Commit()
}

// RenewedNode is one automatic renewal: the passed date and the date it moved to.
type RenewedNode struct{ NodeID, From, To string }

// AutoRenew moves every passed renewal date that has auto renewal forward by
// whole cycles until it is today or later in the panel time zone; a date equal
// to today is not passed yet. Every step counts from the stored date, so a
// month-end date clamps (01-31 → 02-28) instead of spilling into the next
// month. record runs inside the same transaction for each renewal.
func (r *Repository) AutoRenew(ctx context.Context, now time.Time, location *time.Location, record func(context.Context, *sql.Tx, RenewedNode) error) ([]RenewedNode, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	today := now.In(location).Format("2006-01-02")
	rows, err := tx.QueryContext(ctx, "SELECT m.node_id,m.renew_date,m.renew_cycle FROM node_meta m JOIN nodes n ON n.id=m.node_id WHERE n.deleted_at IS NULL AND m.auto_renew=1 AND m.renew_date<?", today)
	if err != nil {
		return nil, err
	}
	type due struct{ id, date, cycle string }
	var items []due
	for rows.Next() {
		var item due
		if err = rows.Scan(&item.id, &item.date, &item.cycle); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	renewed := make([]RenewedNode, 0, len(items))
	for _, item := range items {
		months := renewalCycleMonths[item.cycle]
		if months == 0 {
			return nil, fmt.Errorf("node %s has auto renewal with non-recurring cycle %q", item.id, item.cycle)
		}
		start, err := time.Parse("2006-01-02", item.date)
		if err != nil {
			return nil, fmt.Errorf("node %s renewal date: %w", item.id, err)
		}
		next := item.date
		for step := 1; next < today; step++ {
			next = addMonthsClamped(start, step*months).Format("2006-01-02")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE node_meta SET renew_date=? WHERE node_id=?", next, item.id); err != nil {
			return nil, err
		}
		result := RenewedNode{NodeID: item.id, From: item.date, To: next}
		if err = record(ctx, tx, result); err != nil {
			return nil, err
		}
		renewed = append(renewed, result)
	}
	return renewed, tx.Commit()
}

// addMonthsClamped adds months and keeps the day, using the last day of the
// target month when it is shorter.
func addMonthsClamped(t time.Time, months int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	return time.Date(first.Year(), first.Month(), min(t.Day(), first.AddDate(0, 1, -1).Day()), 0, 0, 0, 0, time.UTC)
}
