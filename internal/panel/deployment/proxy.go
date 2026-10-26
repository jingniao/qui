package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

// ProxyConfiguration is the proxy module's public surface as the Deployment
// module sees it. The quick user-access feature writes its inbound through the
// same service the Owner's inbound pages use, and renders through the same
// renderer as POST /api/v1/revisions/proxy.
// SnapshotIntent is Snapshot plus the inbounds the Owner has enabled, which
// becomes the revision's inbound intent.
type ProxyConfiguration interface {
	Snapshot(context.Context, string, proxy.AccessReader) (desiredstate.State, error)
	SnapshotIntent(context.Context, string, proxy.AccessReader) (desiredstate.State, []string, error)
	SaveInbound(context.Context, string, proxy.InboundInput) (proxy.Inbound, error)
	InboundAtListener(context.Context, string, string, string, int) (proxy.Inbound, error)
}

func (h *Handler) proxyRevision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID  string `json:"nodeId"`
		Summary string `json:"summary"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if h.Proxy == nil || h.Users == nil {
		httpapi.Error(w, 503, "unavailable", "Proxy configuration unavailable")
		return
	}
	if strings.TrimSpace(body.Summary) == "" || len(body.Summary) > 512 {
		httpapi.Error(w, 400, "invalid_request", "Revision summary required")
		return
	}
	state, enabled, err := h.Proxy.SnapshotIntent(r.Context(), body.NodeID, h.Users)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	revision, err := h.Repository.CreateWithInboundIntent(r.Context(), state, enabled, auth.Actor(r.Context()).Username, body.Summary)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "创建代理配置版本", body.NodeID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 201, revision)
}
func (h *Handler) checkRelayTargets(ctx context.Context, state desiredstate.State) error {
	for _, outbound := range state.Outbounds {
		if outbound.TargetNodeID == "" || !outbound.Enabled {
			continue
		}
		target, err := h.Repository.Published(ctx, outbound.TargetNodeID)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("relay target node must be deployed first")
		}
		if err != nil {
			return err
		}
		if reason := relayDependency(outbound, target); reason != "matched" {
			return fmt.Errorf("%w: publish a matching target before source (%s)", ErrDependency, reason)
		}
	}
	return nil
}
