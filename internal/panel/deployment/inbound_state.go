package deployment

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"

	"proxycontrol/internal/panel/node"
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
	var running desiredstate.State
	var runningIntent []string
	var runningRevision int64
	if state, intent, err := r.PublishedWithIntent(ctx, nodeID); err == nil {
		running, runningIntent, runningRevision = state, intent, state.Revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	attempts, err := r.newestAttempts(ctx, nodeID, draft.Inbounds)
	if err != nil {
		return nil, err
	}
	items := make([]InboundState, 0, len(draft.Inbounds))
	for _, want := range draft.Inbounds {
		item := InboundState{InboundID: want.ID, State: InboundUnpublished}
		index := slices.IndexFunc(running.Inbounds, func(have desiredstate.Inbound) bool { return have.ID == want.ID })
		if index >= 0 {
			item.State = InboundModified
			have := running.Inbounds[index]
			if enabledIntent(runningIntent, have) == slices.Contains(draftIntent, want.ID) && sameInboundRuntime(have, want) {
				item.State = InboundEffective
				items = append(items, item)
				continue
			}
		}
		if attempt, ok := attempts[want.ID]; ok {
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
		items = append(items, item)
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
func (r *Repository) newestAttempts(ctx context.Context, nodeID string, inbounds []desiredstate.Inbound) (map[string]inboundAttempt, error) {
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
	wanted := map[string]bool{}
	for _, inbound := range inbounds {
		wanted[inbound.ID] = true
	}
	found := map[string]inboundAttempt{}
	for _, attempt := range history {
		if len(found) == len(wanted) {
			break
		}
		state, err := r.State(ctx, attempt.revision)
		if err != nil {
			return nil, err
		}
		for _, carried := range state.Inbounds {
			if _, seen := found[carried.ID]; wanted[carried.ID] && !seen {
				found[carried.ID] = attempt
			}
		}
	}
	return found, nil
}

func (h *Handler) inboundStates(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	n, err := h.Nodes.Get(r.Context(), nodeID)
	if err != nil || n.DeletedAt != "" {
		if err == nil || errors.Is(err, node.ErrNotFound) {
			httpapi.Error(w, 404, "not_found", "Node not found")
		} else {
			h.fail(w, err)
		}
		return
	}
	if h.Proxy == nil || h.Users == nil {
		h.fail(w, &apiFailure{Status: 503, Code: "unavailable", Message: "Proxy configuration unavailable"})
		return
	}
	draft, intent, err := h.Proxy.SnapshotIntent(r.Context(), nodeID, h.Users)
	if err != nil {
		httpapi.Error(w, 409, "snapshot_unavailable", err.Error())
		return
	}
	items, err := h.Repository.InboundStates(r.Context(), nodeID, draft, intent)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
