package access

import (
	"context"
	"database/sql"
	"encoding/json"
	contract "proxycontrol/pkg/contracts/access"
	"strings"
	"time"
)

type Filter struct {
	NodeID, UserID, DeviceID, Domain string
	Allowed                          *bool
	From, To                         time.Time
}

// Item is one aggregated access record read back for the Owner page. The byte
// counters are nullable: an upstream core cannot measure bytes per domain, so a
// record whose measurement is unknown carries null (and the page shows "—")
// instead of an invented zero (plan D4).
type Item struct {
	ID                string    `json:"id"`
	Bucket            time.Time `json:"bucket"`
	NodeID            string    `json:"node_id"`
	Revision          int64     `json:"revision"`
	UserID            string    `json:"user_id,omitempty"`
	DeviceID          string    `json:"device_id,omitempty"`
	InboundProtocol   string    `json:"inbound_protocol"`
	Domain            string    `json:"domain,omitempty"`
	TargetIP          string    `json:"target_ip,omitempty"`
	TargetPort        int       `json:"target_port"`
	Network           string    `json:"network"`
	RuleID            string    `json:"rule_id,omitempty"`
	Action            string    `json:"action"`
	OutboundID        string    `json:"outbound_id,omitempty"`
	OutboundProtocol  string    `json:"outbound_protocol,omitempty"`
	ChainID           string    `json:"chain_id,omitempty"`
	FinalEgressNodeID string    `json:"final_egress_node_id,omitempty"`
	EgressKnown       bool      `json:"egress_known"`
	Allowed           bool      `json:"allowed"`
	Connections       int64     `json:"connections"`
	UploadBytes       *int64    `json:"upload_bytes"`
	DownloadBytes     *int64    `json:"download_bytes"`
	TrafficKnown      bool      `json:"traffic_known"`
}
type Summary struct {
	Connections               int64 `json:"connections"`
	Blocked                   int64 `json:"blocked"`
	UploadBytes               int64 `json:"uploadBytes"`
	DownloadBytes             int64 `json:"downloadBytes"`
	UnknownTrafficConnections int64 `json:"unknownTrafficConnections"`
}
type Domain struct {
	Domain                    string `json:"domain"`
	TargetIP                  string `json:"targetIp"`
	Connections               int64  `json:"connections"`
	Blocked                   int64  `json:"blocked"`
	UploadBytes               *int64 `json:"uploadBytes"`
	DownloadBytes             *int64 `json:"downloadBytes"`
	UnknownTrafficConnections int64  `json:"unknownTrafficConnections"`
}

func conditions(filter Filter) (string, []any) {
	parts := []string{"bucket>=?", "bucket<?"}
	args := []any{timestamp(filter.From), timestamp(filter.To)}
	for _, entry := range []struct{ column, value string }{{"node_id", filter.NodeID}, {"user_id", filter.UserID}, {"device_id", filter.DeviceID}, {"domain", filter.Domain}} {
		if entry.value != "" {
			parts = append(parts, entry.column+"=?")
			args = append(args, entry.value)
		}
	}
	if filter.Allowed != nil {
		parts = append(parts, "allowed=?")
		args = append(args, *filter.Allowed)
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func (r *Repository) List(ctx context.Context, filter Filter, page, size int) ([]Item, int, error) {
	where, args := conditions(filter)
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM access_rollups"+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, "SELECT id,dimensions_json,connections,upload_bytes,download_bytes FROM access_rollups"+where+" ORDER BY bucket DESC,id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var id, encoded string
		var dimensions contract.Rollup
		var connections int64
		var upload, download sql.NullInt64
		if err := rows.Scan(&id, &encoded, &connections, &upload, &download); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal([]byte(encoded), &dimensions); err != nil {
			return nil, 0, err
		}
		items = append(items, Item{ID: id, Bucket: dimensions.Bucket, NodeID: dimensions.NodeID, Revision: dimensions.Revision, UserID: dimensions.UserID, DeviceID: dimensions.DeviceID, InboundProtocol: dimensions.InboundProtocol, Domain: dimensions.Domain, TargetIP: dimensions.TargetIP, TargetPort: dimensions.TargetPort, Network: dimensions.Network, RuleID: dimensions.RuleID, Action: dimensions.Action, OutboundID: dimensions.OutboundID, OutboundProtocol: dimensions.OutboundProtocol, ChainID: dimensions.ChainID, FinalEgressNodeID: dimensions.FinalEgressNodeID, EgressKnown: dimensions.EgressKnown, Allowed: dimensions.Allowed, Connections: connections, UploadBytes: nullableInt(upload), DownloadBytes: nullableInt(download), TrafficKnown: dimensions.TrafficKnown})
	}
	return items, count, rows.Err()
}

// nullableInt keeps the difference between a measured value and an unknown one:
// a NULL column reads back as nil, which the API serialises as null.
func nullableInt(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func (r *Repository) Summary(ctx context.Context, filter Filter) (Summary, error) {
	where, args := conditions(filter)
	var result Summary
	err := r.Database.QueryRowContext(ctx, `SELECT coalesce(sum(connections),0),coalesce(sum(CASE WHEN allowed=0 THEN connections ELSE 0 END),0),coalesce(sum(upload_bytes),0),coalesce(sum(download_bytes),0),coalesce(sum(CASE WHEN traffic_known=0 THEN connections ELSE 0 END),0) FROM access_rollups`+where, args...).Scan(&result.Connections, &result.Blocked, &result.UploadBytes, &result.DownloadBytes, &result.UnknownTrafficConnections)
	return result, err
}

func (r *Repository) Domains(ctx context.Context, filter Filter, page, size int) ([]Domain, int, error) {
	where, args := conditions(filter)
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT domain,target_ip FROM access_rollups"+where+" GROUP BY domain,target_ip)", args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, `SELECT domain,target_ip,sum(connections),sum(CASE WHEN allowed=0 THEN connections ELSE 0 END),sum(upload_bytes),sum(download_bytes),sum(CASE WHEN traffic_known=0 THEN connections ELSE 0 END) FROM access_rollups`+where+" GROUP BY domain,target_ip ORDER BY sum(connections) DESC,domain,target_ip LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Domain{}
	for rows.Next() {
		var item Domain
		var upload, download sql.NullInt64
		if err := rows.Scan(&item.Domain, &item.TargetIP, &item.Connections, &item.Blocked, &upload, &download, &item.UnknownTrafficConnections); err != nil {
			return nil, 0, err
		}
		item.UploadBytes, item.DownloadBytes = nullableInt(upload), nullableInt(download)
		items = append(items, item)
	}
	return items, count, rows.Err()
}

func (r *Repository) Prune(ctx context.Context, now time.Time, retention Retention) error {
	if err := retention.Validate(); err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := pruneInTx(ctx, tx, now.UTC(), retention); err != nil {
		return err
	}
	return tx.Commit()
}
