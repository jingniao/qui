package deployment

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

// CanvasPublisher publishes the node snapshots of a relay-canvas scheme through
// the batches this module already owns, so a drawing reaches the nodes exactly
// like a chain or relay publication does: one batch, targets first, one revision
// per node, applied by the Agents. A drawing that touches a single node is a
// plain single-node revision, because a batch needs at least two nodes.
//
// A drawing also captures the relay component around the nodes it names, because
// the drawing depends on it. Those extra nodes are only redeployed when their
// configuration actually changed: a node that already runs the captured state
// and inbound intent gets no revision, no batch item and no rollback item, so a
// tunnel between two hosts no longer restarts every other host that relays
// through them.
type CanvasPublisher struct {
	Repository *Repository
}

func (p *CanvasPublisher) PublishCanvas(ctx context.Context, name, author string, snapshots []proxy.NodeSnapshot) (proxy.CanvasPublishResult, error) {
	result := proxy.CanvasPublishResult{Nodes: []proxy.CanvasPublishNode{}, Warnings: []string{}}
	if len(snapshots) == 0 {
		return result, errors.New("no nodes to publish")
	}
	retained, err := p.retainChangedSnapshots(ctx, snapshots)
	if err != nil {
		return result, err
	}
	if len(retained) == 0 {
		return result, nil
	}
	if len(retained) == 1 {
		snapshot := retained[0]
		// The same dependency rule as a publication from the node page: a relay
		// outbound may only go out once its target runs the matching inbound.
		if err := checkRelayTargets(ctx, p.Repository, snapshot.State); err != nil {
			return result, err
		}
		revision, err := p.Repository.CreateWithInboundIntent(ctx, snapshot.State, snapshot.InboundIntent, author, name)
		if err != nil {
			return result, err
		}
		// Preserve the revision identity before starting work: callers that
		// compensate a failed status read must still know what was submitted.
		result.Nodes = append(result.Nodes, proxy.CanvasPublishNode{NodeID: snapshot.State.NodeID, Revision: revision.ID, Status: "draft"})
		if err := p.Repository.Deploy(ctx, revision.ID); err != nil {
			return result, err
		}
		result.Nodes[0].Status = "pending"
		status, err := p.Repository.TargetStatus(ctx, snapshot.State.NodeID, revision.ID)
		if err != nil {
			return result, err
		}
		result.Nodes[0].Status = status
		return result, nil
	}
	batch, err := p.Repository.CreateBatch(ctx, "", name, author, retained)
	if err != nil {
		return result, err
	}
	result.BatchID = batch.ID
	if err := p.Repository.StartBatch(ctx, batch.ID); err != nil {
		return result, err
	}
	started, err := p.Repository.Batch(ctx, batch.ID)
	if err != nil {
		return result, err
	}
	for _, item := range started.Items {
		result.Nodes = append(result.Nodes, proxy.CanvasPublishNode{NodeID: item.NodeID, Revision: item.Revision, Status: item.Status})
	}
	return result, nil
}

// retainChangedSnapshots validates the complete captured set and then drops the
// snapshots the drawing only expanded: they already run exactly the captured
// state and inbound intent, so publishing them would restart their cores for
// nothing. The order of the retained snapshots is unchanged. The whole set is
// validated first, because the drawing depends on all of it - a source whose
// captured target does not serve the listener it dials is still refused.
func (p *CanvasPublisher) retainChangedSnapshots(ctx context.Context, snapshots []proxy.NodeSnapshot) ([]proxy.NodeSnapshot, error) {
	if err := p.validateCapturedSet(ctx, snapshots); err != nil {
		return nil, err
	}
	retained := make([]proxy.NodeSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !snapshot.ExpandedOnly {
			retained = append(retained, snapshot)
			continue
		}
		published, intent, err := p.Repository.PublishedWithIntent(ctx, snapshot.State.NodeID)
		if errors.Is(err, sql.ErrNoRows) {
			// Nothing is running on it yet: it has to be published.
			retained = append(retained, snapshot)
			continue
		}
		if err != nil {
			return nil, err
		}
		if !sameDesiredState(published, snapshot.State) || !sameInboundIntent(intent, snapshot.InboundIntent) {
			retained = append(retained, snapshot)
		}
	}
	return retained, nil
}

// validateCapturedSet applies the batch dependency rule to the whole captured
// set in one transaction, exactly like a batch creation does.
func (p *CanvasPublisher) validateCapturedSet(ctx context.Context, snapshots []proxy.NodeSnapshot) error {
	tx, err := p.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return p.Repository.validateBatchDependencies(ctx, tx, snapshots)
}

// sameDesiredState compares two rendered states ignoring the revision label,
// which only names the publication.
//
// The user access collections are sets, not sequences, and their order legitimately
// differs. An automatic access revision keeps the published order of the
// credentials, the inbound bindings and the WireGuard peers and appends the ones
// that came back, so a user that was disabled and re-enabled lands behind the
// others; a fresh render emits them in the order it reads the access data. Both
// sides are therefore canonicalized before they are compared, on copies: the
// callers' states are never edited. Everything that is genuinely ordered -
// policies, proxy chains, outbounds, outbound groups and the inbound list itself
// - is left exactly as it is.
func sameDesiredState(left, right desiredstate.State) bool {
	left, right = canonicalState(left), canonicalState(right)
	first, err := json.Marshal(left)
	if err != nil {
		return false
	}
	second, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(first, second)
}

// canonicalState drops the revision label, sorts the sets of the state by identity
// and gives their empty case one representation. Slices are cloned before they are
// sorted, so the argument - and the published state the caller still uses - keeps
// its order.
func canonicalState(state desiredstate.State) desiredstate.State {
	state.Revision = 0
	state.Users = canonicalSet(state.Users, func(a, b desiredstate.User) int { return strings.Compare(a.ID, b.ID) })
	state.Credentials = canonicalSet(state.Credentials, func(a, b desiredstate.Credential) int { return strings.Compare(a.ID, b.ID) })
	state.Inbounds = slices.Clone(state.Inbounds)
	for index := range state.Inbounds {
		inbound := &state.Inbounds[index]
		inbound.CredentialIDs = canonicalSet(inbound.CredentialIDs, strings.Compare)
		// A WireGuard inbound carries one peer per credential, and the peer list
		// is rebuilt from the same bindings, so it is the same set.
		wireguard := inbound.Settings.WireGuard
		if wireguard == nil {
			continue
		}
		copied := *wireguard
		copied.Peers = canonicalSet(copied.Peers, func(a, b desiredstate.WireGuardPeer) int { return strings.Compare(a.CredentialID, b.CredentialID) })
		inbound.Settings.WireGuard = &copied
	}
	return state
}

// canonicalSet gives an empty set one representation. An access revision that is
// rebuilt from the published one publishes an allocated but empty list - peers:
// [] once the last user of a WireGuard inbound is disabled - while a fresh render
// emits nothing at all, which encodes as null. Both are the same set, and only
// the encoded bytes would tell them apart. A non-empty set is cloned and sorted
// by the given comparison, so the caller's slice keeps its order.
func canonicalSet[T any](items []T, compare func(a, b T) int) []T {
	if len(items) == 0 {
		return nil
	}
	cloned := slices.Clone(items)
	slices.SortFunc(cloned, compare)
	return cloned
}

// sameInboundIntent reports whether the captured intent is the published one. An
// unknown (nil) published intent - a revision from before the column - never
// compares equal to a set, empty or not.
func sameInboundIntent(published, captured []string) bool {
	return published != nil && captured != nil && slices.Equal(published, captured)
}
