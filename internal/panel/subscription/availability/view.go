package availability

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
)

type Store struct {
	Database *sql.DB
	Vault    *vault.Vault
}

type Fact struct {
	ID, Name, Status, LastHeartbeat string
	Maintenance                     bool
	Disabled                        bool
}

// View fixes the time and all status inputs for one subscription generation.
type View struct {
	Now                 time.Time
	OfflineAfterSeconds int
	Nodes               map[string]Fact
}

type Exclusion struct{ NodeID, NodeName, Reason string }

func (v View) Evaluate(ids []string) ([]Exclusion, error) {
	var excluded []Exclusion
	for _, id := range Unique(ids) {
		n, ok := v.Nodes[id]
		if !ok {
			return nil, fmt.Errorf("subscription dependency node %s: status record missing", id)
		}
		reason := ""
		if n.Disabled || n.Status == "disabled" {
			reason = "已禁用"
		} else if n.Maintenance {
			reason = "维护中"
		} else if node.ConfirmedOffline(n.Status, n.LastHeartbeat, v.OfflineAfterSeconds, v.Now) {
			reason = "确认离线"
		}
		if reason != "" {
			excluded = append(excluded, Exclusion{id, n.Name, reason})
		}
	}
	return excluded, nil
}

func readView(ctx context.Context, q Reader, now time.Time) (View, error) {
	v := View{Now: now, Nodes: map[string]Fact{}}
	if err := q.QueryRowContext(ctx, `SELECT duration_minutes*60 FROM notification_rules WHERE kind='node_offline'`).Scan(&v.OfflineAfterSeconds); err != nil {
		return v, fmt.Errorf("subscription offline duration: %w", err)
	}
	rows, err := q.QueryContext(ctx, `SELECT id,name,status,last_heartbeat,maintenance,NOT enabled FROM nodes`)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var fact Fact
		if err = rows.Scan(&fact.ID, &fact.Name, &fact.Status, &fact.LastHeartbeat, &fact.Maintenance, &fact.Disabled); err != nil {
			return v, err
		}
		v.Nodes[fact.ID] = fact
	}
	return v, rows.Err()
}

func (s *Store) Filter(ctx context.Context, entries []proxy.Proxy) ([]proxy.Proxy, []proxy.Warning, error) {
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	v, err := readView(ctx, tx, time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}
	g, err := LoadGraph(ctx, tx, s.Vault)
	if err != nil {
		return nil, nil, err
	}
	result := make([]proxy.Proxy, 0, len(entries))
	var warnings []proxy.Warning
	cache := map[string][]string{}
	for _, p := range entries {
		if p.Origin.Source != "published" {
			result = append(result, p)
			continue
		}
		ref := "inbound:" + p.Origin.Ref
		if p.Origin.Relayed {
			ref = "relay:" + p.Origin.Ref
		}
		ids, known := cache[ref]
		if !known {
			if p.Origin.Relayed {
				record, e := Read(ctx, tx, ref)
				if e != nil {
					return nil, nil, e
				}
				var snapshot string
				if e = tx.QueryRowContext(ctx, `SELECT published_snapshot FROM tunnel_rules WHERE id=?`, p.Origin.Ref).Scan(&snapshot); e != nil {
					return nil, nil, e
				}
				digest, e := RelayDigest([]byte(snapshot))
				if e != nil {
					return nil, nil, e
				}
				if digest != record.SourceDigest {
					return nil, nil, fmt.Errorf("subscription route %s: successful snapshot digest mismatch", ref)
				}
				var proof struct {
					Entry        proxy.Ref
					InboundID    string
					RouteNodeIDs []string `json:"routeNodeIds"`
				}
				if e = json.Unmarshal([]byte(snapshot), &proof); e != nil {
					return nil, nil, e
				}
				if proof.Entry.ID != record.OwnerNodeID || proof.InboundID != record.TargetInboundID || !slices.Equal(Unique(proof.RouteNodeIDs), Unique(record.PathNodeIDs)) {
					return nil, nil, fmt.Errorf("subscription route %s: dependency index does not match successful route proof", ref)
				}
				ids = append(ids, record.PathNodeIDs...)
				if record.TargetInboundID != "" {
					next, e := g.Route(record.TargetInboundID)
					if e != nil {
						return nil, nil, fmt.Errorf("subscription route %s: %w", ref, e)
					}
					ids = append(ids, next...)
				}
			} else {
				record, e := Read(ctx, tx, ref)
				if e != nil {
					return nil, nil, e
				}
				state, _, e := g.Inbound(p.Origin.Ref)
				if e != nil {
					return nil, nil, e
				}
				digest, e := Digest(state)
				if e != nil {
					return nil, nil, e
				}
				if digest != record.SourceDigest {
					return nil, nil, fmt.Errorf("subscription route %s: confirmed revision digest mismatch", ref)
				}
				ids, err = g.Route(p.Origin.Ref)
				if err != nil {
					return nil, nil, err
				}
			}
			cache[ref] = ids
		}
		excluded, e := v.Evaluate(ids)
		if e != nil {
			return nil, nil, e
		}
		if len(excluded) == 0 {
			result = append(result, p)
			continue
		}
		var reasons []string
		for _, item := range excluded {
			reasons = append(reasons, "服务器「"+item.NodeName+"」（"+item.NodeID+"）"+item.Reason)
		}
		warnings = append(warnings, proxy.SourceWarning("availability", "线路「"+p.Name+"」（"+ref+"）已从订阅排除："+strings.Join(reasons, "；")))
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return result, warnings, nil
}
