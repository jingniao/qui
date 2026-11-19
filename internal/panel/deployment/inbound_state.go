package deployment

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Publish states of one inbound, relative to the revision its node runs.
const (
	InboundEffective   = "effective"   // the running revision carries the inbound as the draft has it
	InboundModified    = "modified"    // the running revision carries it, the draft differs
	InboundPending     = "pending"     // a newer revision with it waits for the node
	InboundUnpublished = "unpublished" // the running revision does not carry it
)

// inboundStateHistory bounds how many of a node's latest deployments are read
// to find the newest one that carried an inbound.
const inboundStateHistory = 50

// InboundState is one inbound's publish state. Code and Revision are set only
// for an unpublished inbound whose newest publication attempt failed.
type InboundState struct {
	InboundID string `json:"inboundId"`
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	Revision  int64  `json:"revision,omitempty"`
}

type inboundAttempt struct {
	revision int64
	status   string
	code     string
}

// InboundStates compares the draft inbounds of a node (rendered by the Proxy
// module, with the Owner's enabled intent) against the revision the node runs.
//
// The comparison covers the inbound itself - listener, protocol, settings,
// certificate material, default outbound - and whether the Owner wants it enabled
// (the revision's intent, or the published Enabled flag when no intent was
// recorded). User credentials, WireGuard peers and the renderer's auto-disable of
// an inbound without users are excluded: they change with user access, and the
// automatic quota revisions publish those on their own, so they never make an
// inbound count as modified. Read-only.
func (r *Repository) InboundStates(ctx context.Context, nodeID string, draft desiredstate.State, draftIntent []string) ([]InboundState, error) {
	items := make([]InboundState, 0, len(draft.Inbounds))
	if len(draft.Inbounds) == 0 {
		return items, nil
	}
	var running desiredstate.State
	var runningIntent []string
	var runningRevision int64
	if state, intent, err := r.PublishedWithIntent(ctx, nodeID); err == nil {
		running, runningIntent, runningRevision = state, intent, state.Revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	byID := make(map[string]desiredstate.Inbound, len(running.Inbounds))
	for _, in := range running.Inbounds {
		byID[in.ID] = in
	}
	wanted := map[string]bool{}
	for _, want := range draft.Inbounds {
		item := InboundState{InboundID: want.ID, State: InboundUnpublished}
		if have, ok := byID[want.ID]; ok {
			item.State = InboundModified
			if enabledIntent(runningIntent, have) == slices.Contains(draftIntent, want.ID) && sameInboundRuntime(have, want) {
				item.State = InboundEffective
			}
		}
		items = append(items, item)
		if item.State != InboundEffective {
			wanted[want.ID] = true
		}
	}
	if len(wanted) == 0 {
		return items, nil
	}
	attempts, err := r.newestAttempts(ctx, nodeID, wanted, running)
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := &items[i]
		if attempt, ok := attempts[item.InboundID]; ok {
			switch {
			case attempt.status == "pending" && attempt.revision > runningRevision:
				item.State = InboundPending
			case item.State == InboundUnpublished && (attempt.status == "failed" || attempt.status == "rollback"):
				item.Revision = attempt.revision
				item.Code = attempt.code
				if item.Code == "" {
					item.Code = attempt.status
				}
			}
		}
	}
	return items, nil
}

// enabledIntent is whether the Owner wanted the published inbound enabled.
func enabledIntent(intent []string, published desiredstate.Inbound) bool {
	if intent == nil {
		return published.Enabled
	}
	return slices.Contains(intent, published.ID)
}

// sameInboundRuntime reuses the whole-state comparison on one inbound, with the
// access-dependent parts removed from both sides.
func sameInboundRuntime(published, draft desiredstate.Inbound) bool {
	return sameDesiredState(desiredstate.State{Inbounds: []desiredstate.Inbound{withoutAccess(published)}}, desiredstate.State{Inbounds: []desiredstate.Inbound{withoutAccess(draft)}})
}

func withoutAccess(inbound desiredstate.Inbound) desiredstate.Inbound {
	inbound.Enabled = false
	inbound.CredentialIDs = nil
	if wireguard := inbound.Settings.WireGuard; wireguard != nil {
		copied := *wireguard
		copied.Peers = nil
		inbound.Settings.WireGuard = &copied
	}
	return inbound
}

// newestAttempts finds, for each inbound, the node's newest deployment whose
// revision carries it. Revisions are opened newest first and only until every
// inbound has been found.
func (r *Repository) newestAttempts(ctx context.Context, nodeID string, wanted map[string]bool, running desiredstate.State) (map[string]inboundAttempt, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT revision,status,result_code FROM deployment_targets WHERE node_id=? ORDER BY revision DESC LIMIT ?", nodeID, inboundStateHistory)
	if err != nil {
		return nil, err
	}
	var history []inboundAttempt
	for rows.Next() {
		var attempt inboundAttempt
		if err := rows.Scan(&attempt.revision, &attempt.status, &attempt.code); err != nil {
			rows.Close()
			return nil, err
		}
		history = append(history, attempt)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	found := map[string]inboundAttempt{}
	for _, attempt := range history {
		if len(found) == len(wanted) {
			break
		}
		state := running
		if attempt.revision != running.Revision {
			state, err = r.State(ctx, attempt.revision)
			if err != nil {
				return nil, err
			}
		}
		for _, carried := range state.Inbounds {
			if _, seen := found[carried.ID]; wanted[carried.ID] && !seen {
				found[carried.ID] = attempt
			}
		}
	}
	return found, nil
}

func (h *Handler) readInboundStates(ctx context.Context, nodeID string) ([]InboundState, error) {
	var exists int
	err := h.Nodes.Database.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id=? AND deleted_at IS NULL", nodeID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apiFailure{Status: 404, Code: "not_found", Message: "Node not found"}
	}
	if err != nil {
		return nil, err
	}
	if h.Proxy == nil {
		return nil, &apiFailure{Status: 503, Code: "unavailable", Message: "Proxy configuration unavailable"}
	}
	draft, intent, err := h.Proxy.InboundDraft(ctx, nodeID)
	if err != nil {
		return nil, &apiFailure{Status: 409, Code: "snapshot_unavailable", Message: err.Error()}
	}
	return h.Repository.InboundStates(ctx, nodeID, draft, intent)
}

func (h *Handler) inboundStates(w http.ResponseWriter, r *http.Request) {
	items, err := h.readInboundStates(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) inboundStatesBatch(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.URL.Query().Get("nodeIds"), ",")
	if len(ids) > 200 {
		httpapi.Error(w, 400, "invalid_request", "Too many node IDs")
		return
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 128 || seen[id] {
			httpapi.Error(w, 400, "invalid_request", "Invalid or duplicate node ID")
			return
		}
		seen[id] = true
	}
	type entry struct {
		NodeID string         `json:"nodeId"`
		Items  []InboundState `json:"items"`
		Error  string         `json:"error,omitempty"`
	}
	items := []entry{}
	for _, id := range ids {
		states, err := h.readInboundStates(r.Context(), id)
		item := entry{NodeID: id, Items: states}
		if err != nil {
			item.Error = err.Error()
		}
		items = append(items, item)
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
