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
	if strings.TrimSpace(body.Summary) == "" || len(body.Summary) > 512 {
		httpapi.Error(w, 400, "invalid_request", "Revision summary required")
		return
	}
	revision, err := h.createProxyRevision(r.Context(), body.NodeID, body.Summary, auth.Actor(r.Context()).Username)
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

// createProxyRevision renders the node's configuration from its business data
// and stores it as a draft together with the inbounds the Owner has enabled.
// POST /api/v1/revisions/proxy and the one-step publication both go through it,
// so the two paths always create the same revision.
func (h *Handler) createProxyRevision(ctx context.Context, nodeID, summary, author string) (Revision, error) {
	if h.Proxy == nil || h.Users == nil {
		return Revision{}, &apiFailure{Status: 503, Code: "unavailable", Message: "Proxy configuration unavailable"}
	}
	state, enabled, err := h.Proxy.SnapshotIntent(ctx, nodeID, h.Users)
	if err != nil {
		return Revision{}, &apiFailure{Status: 400, Code: "invalid_request", Message: err.Error()}
	}
	return h.Repository.CreateWithInboundIntent(ctx, state, enabled, author, summary)
}
func (h *Handler) checkRelayTargets(ctx context.Context, state desiredstate.State) error {
	return checkRelayTargets(ctx, h.Repository, state)
}

// checkRelayTargets refuses a node state whose relay outbounds point at a target
// that has not published the matching inbound yet. Every single-node deployment
// path runs it: the revision endpoint, the one-step publication and a one-node
// canvas publication.
func checkRelayTargets(ctx context.Context, repository *Repository, state desiredstate.State) error {
	for _, outbound := range state.Outbounds {
		if outbound.TargetNodeID == "" || !outbound.Enabled {
			continue
		}
		target, err := repository.Published(ctx, outbound.TargetNodeID)
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
