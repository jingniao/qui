package deployment

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

type publishedRelay struct {
	OutboundID       string `json:"outboundId"`
	TargetNodeID     string `json:"targetNodeId"`
	Protocol         string `json:"protocol"`
	TargetRevision   int64  `json:"targetRevision"`
	Dependency       string `json:"dependency"`
	DependencyDetail string `json:"dependencyDetail,omitempty"`
}
type publishedTopology struct {
	NodeID           string                    `json:"nodeId"`
	Revision         int64                     `json:"revision"`
	ReportedRevision int64                     `json:"reportedRevision"`
	Observation      string                    `json:"observation"`
	Snapshot         string                    `json:"snapshot"`
	Relays           []publishedRelay          `json:"relays"`
	Chains           []desiredstate.ProxyChain `json:"chains"`
}

// Published topology is derived exclusively from acknowledged runtime snapshots.
// Credentials and protocol settings are used only for local dependency matching.
func (h *Handler) publishedTopology(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_pagination", "Invalid pagination")
		return
	}
	tx, err := h.Repository.Database.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	var total int
	if err := tx.QueryRowContext(r.Context(), "SELECT count(*) FROM nodes WHERE deleted_at IS NULL").Scan(&total); err != nil {
		h.fail(w, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT n.id,coalesce(s.revision,0),n.enabled,n.status,n.last_heartbeat,n.heartbeat_json FROM nodes n LEFT JOIN node_runtime_state s ON s.node_id=n.id WHERE n.deleted_at IS NULL ORDER BY n.created_at,n.id LIMIT ? OFFSET ?`, size, (page-1)*size)
	if err != nil {
		h.fail(w, err)
		return
	}
	type entry struct {
		id, status, last, heartbeat string
		revision                    int64
		enabled                     bool
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.revision, &e.enabled, &e.status, &e.last, &e.heartbeat); err != nil {
			rows.Close()
			h.fail(w, err)
			return
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		h.fail(w, err)
		return
	}
	items := []publishedTopology{}
	at := time.Now().UTC()
	for _, e := range entries {
		item := publishedTopology{NodeID: e.id, Revision: e.revision, Observation: "unknown", Snapshot: "unpublished", Relays: []publishedRelay{}, Chains: []desiredstate.ProxyChain{}}
		var heartbeat struct {
			Revision int64 `json:"current_revision"`
		}
		if json.Unmarshal([]byte(e.heartbeat), &heartbeat) == nil {
			item.ReportedRevision = heartbeat.Revision
		}
		if !e.enabled {
			item.Observation = "disabled"
		} else if observed, err := time.Parse(time.RFC3339Nano, e.last); err == nil && !observed.After(at) && at.Sub(observed) <= 45*time.Second && (e.status == "online" || e.status == "degraded") {
			item.Observation = "fresh_control"
			if item.ReportedRevision != e.revision {
				item.Observation = "revision_mismatch"
			}
		} else {
			item.Observation = "stale_control"
		}
		if e.revision > 0 {
			state, err := h.Repository.stateInTransaction(r.Context(), tx, e.revision)
			if err != nil || state.NodeID != e.id {
				item.Snapshot = "unavailable"
			} else {
				item.Snapshot = "available"
				item.Chains = append(item.Chains, state.ProxyChains...)
				for _, out := range state.Outbounds {
					if !out.Enabled || out.TargetNodeID == "" {
						continue
					}
					relay := publishedRelay{OutboundID: out.ID, TargetNodeID: out.TargetNodeID, Protocol: out.Protocol, Dependency: "unpublished"}
					var enabled bool
					err := tx.QueryRowContext(r.Context(), `SELECT n.enabled,coalesce(s.revision,0) FROM nodes n LEFT JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=?`, out.TargetNodeID).Scan(&enabled, &relay.TargetRevision)
					if errors.Is(err, sql.ErrNoRows) {
						relay.Dependency = "missing_node"
					} else if err != nil {
						h.fail(w, err)
						return
					} else if !enabled {
						relay.Dependency = "disabled"
					} else if relay.TargetRevision > 0 {
						target, err := h.Repository.stateInTransaction(r.Context(), tx, relay.TargetRevision)
						if err != nil || target.NodeID != out.TargetNodeID {
							relay.Dependency = "unavailable"
						} else {
							relay.DependencyDetail = relayDependency(out, target)
							relay.Dependency = "mismatch"
							if relay.DependencyDetail == "matched" {
								relay.Dependency = "matched"
							}
						}
					}
					item.Relays = append(item.Relays, relay)
				}
			}
		}
		items = append(items, item)
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size, "observedAt": now()})
}
