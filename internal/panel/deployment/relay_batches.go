package deployment

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/httpapi"
)

type RelayConfiguration interface {
	RelaySnapshots(context.Context, string, proxy.AccessReader) ([]proxy.NodeSnapshot, error)
}

func (h *Handler) createRelayBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID  string `json:"nodeId"`
		Summary string `json:"summary"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.NodeID == "" || strings.TrimSpace(body.Summary) == "" || len(body.Summary) > 512 {
		httpapi.Error(w, 400, "invalid_request", "Node and revision summary required")
		return
	}
	planner, ok := h.Proxy.(RelayConfiguration)
	if !ok || h.Users == nil {
		httpapi.Error(w, 503, "unavailable", "Relay configuration unavailable")
		return
	}
	snapshots, err := planner.RelaySnapshots(r.Context(), body.NodeID, h.Users)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	batch, err := h.Repository.CreateBatch(r.Context(), "", body.Summary, auth.Actor(r.Context()).Username, snapshots)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "创建关联节点发布批次", batch.ID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 201, batch)
}

// Validate all captured dependencies before creating any batch revisions.
func validateBatchDependencies(snapshots []proxy.NodeSnapshot) error {
	positions := map[string]int{}
	for position, snapshot := range snapshots {
		nodeID := snapshot.State.NodeID
		if _, duplicate := positions[nodeID]; duplicate || nodeID == "" {
			return fmt.Errorf("%w: invalid duplicate batch node", ErrDependency)
		}
		positions[nodeID] = position
	}
	for position, snapshot := range snapshots {
		for _, outbound := range snapshot.State.Outbounds {
			if !outbound.Enabled || outbound.TargetNodeID == "" {
				continue
			}
			targetPosition, exists := positions[outbound.TargetNodeID]
			if !exists || targetPosition >= position {
				return fmt.Errorf("%w: batch must contain each relay target before its source", ErrDependency)
			}
			if reason := relayDependency(outbound, snapshots[targetPosition].State); reason != "matched" {
				return fmt.Errorf("%w: captured batch relay mismatch (%s)", ErrDependency, reason)
			}
		}
	}
	return nil
}
