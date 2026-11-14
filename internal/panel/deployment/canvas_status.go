package deployment

import (
	"context"
	"proxycontrol/internal/panel/proxy"
)

// CanvasBatch adapts the existing deployment read API without exposing its
// persistence schema to the canvas status projection.
func (p *CanvasPublisher) CanvasBatch(ctx context.Context, batchID string) (proxy.CanvasStatusBatch, error) {
	batch, err := p.Repository.Batch(ctx, batchID)
	if err != nil {
		return proxy.CanvasStatusBatch{}, err
	}
	result := proxy.CanvasStatusBatch{ID: batch.ID, Status: batch.Status, Items: []proxy.CanvasStatusBatchItem{}}
	for _, item := range batch.Items {
		result.Items = append(result.Items, proxy.CanvasStatusBatchItem{NodeID: item.NodeID, Revision: item.Revision, Status: item.Status})
	}
	return result, nil
}
