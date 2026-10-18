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

type ProxyConfiguration interface {
	Snapshot(context.Context, string, proxy.AccessReader) (desiredstate.State, error)
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
	state, err := h.Proxy.Snapshot(r.Context(), body.NodeID, h.Users)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	revision, err := h.Repository.Create(r.Context(), state, auth.Actor(r.Context()).Username, body.Summary)
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
