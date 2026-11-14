package deployment

import (
	"net/http"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
)

// baselineSummary marks the revision publishBaseline creates.
const baselineSummary = "auto: baseline before upgrade"

// publishBaseline gives an online node that never ran a configuration its first
// confirmed revision (v1.5.5 item 11). Agent and core updates are bound to the
// node's confirmed revision, which must be at least 1, so such a node could not
// be upgraded from the web. The baseline holds no inbound, user, outbound
// resource, policy or relay and never reads the node's drafts: the cores stay
// stopped and no user is affected. It is deployed like any revision, so the
// Agent validates it and rolls back on failure; the page queues the upgrade
// once the node confirms it.
func (h *Handler) publishBaseline(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	var body struct {
		ConfirmNodeID string `json:"confirmNodeId"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.ConfirmNodeID != nodeID {
		httpapi.Error(w, 400, "invalid_request", "confirmNodeId must match the node")
		return
	}
	n, err := h.Nodes.Get(r.Context(), nodeID)
	if err != nil || !n.Enabled || n.DeletedAt != "" {
		httpapi.Error(w, 404, "not_found", "Node not found or disabled")
		return
	}
	if n.Status != "online" && n.Status != "degraded" {
		httpapi.Error(w, 409, "node_offline", "节点离线或心跳已过期，无法发布基础配置。")
		return
	}
	current, err := h.Repository.Current(r.Context(), nodeID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if current > 0 {
		httpapi.Error(w, 409, "baseline_not_needed", "节点已有已确认的配置版本，可以直接升级。")
		return
	}
	state := desiredstate.Baseline(nodeID, desiredstate.NetworkOrNil(n.OutboundSourceIP, n.OutboundInterface, n.HostSourceIPv4, n.HostSourceIPv6))
	if err := state.Validate(); err != nil {
		h.fail(w, err)
		return
	}
	actor := auth.Actor(r.Context()).Username
	revision, err := h.Repository.CreateWithInboundIntent(r.Context(), state, []string{}, actor, baselineSummary)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.deployRevision(r.Context(), revision.ID); err != nil {
		h.fail(w, withRevision(err, revision.ID))
		return
	}
	if err := h.Audit.Record(r.Context(), actor, "升级前发布基础配置", nodeID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"revision": revision.ID, "status": "pending"})
}
