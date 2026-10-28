package deployment

import (
	"context"
	"errors"

	"proxycontrol/internal/panel/proxy"
)

// CanvasPublisher publishes the node snapshots of a relay-canvas scheme through
// the batches this module already owns, so a drawing reaches the nodes exactly
// like a chain or relay publication does: one batch, targets first, one revision
// per node, applied by the Agents. A drawing that touches a single node is a
// plain single-node revision, because a batch needs at least two nodes.
type CanvasPublisher struct {
	Repository *Repository
}

func (p *CanvasPublisher) PublishCanvas(ctx context.Context, name, author string, snapshots []proxy.NodeSnapshot) (proxy.CanvasPublishResult, error) {
	result := proxy.CanvasPublishResult{Nodes: []proxy.CanvasPublishNode{}, Warnings: []string{}}
	if len(snapshots) == 0 {
		return result, errors.New("no nodes to publish")
	}
	if len(snapshots) == 1 {
		snapshot := snapshots[0]
		// The same dependency rule as a publication from the node page: a relay
		// outbound may only go out once its target runs the matching inbound.
		if err := checkRelayTargets(ctx, p.Repository, snapshot.State); err != nil {
			return result, err
		}
		revision, err := p.Repository.CreateWithInboundIntent(ctx, snapshot.State, snapshot.InboundIntent, author, name)
		if err != nil {
			return result, err
		}
		if err := p.Repository.Deploy(ctx, revision.ID); err != nil {
			return result, err
		}
		status, err := p.Repository.TargetStatus(ctx, snapshot.State.NodeID, revision.ID)
		if err != nil {
			return result, err
		}
		result.Nodes = append(result.Nodes, proxy.CanvasPublishNode{NodeID: snapshot.State.NodeID, Revision: revision.ID, Status: status})
		return result, nil
	}
	batch, err := p.Repository.CreateBatch(ctx, "", name, author, snapshots)
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
