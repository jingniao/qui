package deployment

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

type UserAccess interface {
	AccessForGroups(context.Context, []string) ([]user.Access, error)
}
type UserAccessInput struct {
	NodeID   string `json:"nodeId"`
	Summary  string `json:"summary"`
	Protocol string `json:"protocol"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
}

func (h *Handler) userAccess(w http.ResponseWriter, r *http.Request) {
	var input UserAccessInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if h.Users == nil || h.Proxy == nil {
		httpapi.Error(w, 503, "unavailable", "User access service unavailable")
		return
	}
	state, enabled, err := h.EnsureUserAccess(r.Context(), input)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	revision, err := h.Repository.CreateWithInboundIntent(r.Context(), state, enabled, auth.Actor(r.Context()).Username, input.Summary)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "创建用户接入配置", input.NodeID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 201, revision)
}

// EnsureUserAccess writes the quick user-access feature's business data and
// returns the node's rendered configuration together with the inbounds the Owner
// has enabled, which the caller records as the revision's inbound intent.
//
// The feature owns one subscription-visible inbound per listener, bound to the
// users of the node's subscription groups that hold a credential for the
// requested protocol. It is a real inbounds row, not a state only the revision
// knows about: the proxy renderer, the publisher and the automatic quota
// revisions all read that row, so a later publication can neither lose the
// user's credential nor wipe the rest of the node's configuration.
func (h *Handler) EnsureUserAccess(ctx context.Context, input UserAccessInput) (desiredstate.State, []string, error) {
	if strings.TrimSpace(input.Summary) == "" || len(input.Summary) > 512 {
		return desiredstate.State{}, nil, errors.New("revision summary required")
	}
	if input.Protocol != "http" && input.Protocol != "socks5" {
		return desiredstate.State{}, nil, errors.New("user access bootstrap supports HTTP and SOCKS5")
	}
	if h.Proxy == nil || h.Users == nil {
		return desiredstate.State{}, nil, errors.New("user access service unavailable")
	}
	n, err := h.Nodes.Get(ctx, input.NodeID)
	if err != nil || !n.Enabled {
		return desiredstate.State{}, nil, errors.New("target node missing or disabled")
	}
	groups, err := h.Nodes.SubscriptionGroups(ctx, n.ID)
	if err != nil {
		return desiredstate.State{}, nil, err
	}
	access, err := h.Users.AccessForGroups(ctx, groups)
	if err != nil {
		return desiredstate.State{}, nil, err
	}
	// The inbound is bound to the same users the feature has always selected:
	// every granted user that holds a credential for the requested protocol.
	userIDs := []string{}
	for _, entry := range access {
		for _, credential := range entry.Credentials {
			if credential.Protocol == input.Protocol {
				userIDs = append(userIDs, entry.User.ID)
				break
			}
		}
	}
	if err := h.bindUserAccessInbound(ctx, input, userIDs); err != nil {
		return desiredstate.State{}, nil, err
	}
	// Render through the module that owns the proxy configuration, exactly like
	// POST /api/v1/revisions/proxy, so the revision preserves the Owner's other
	// inbounds, outbounds, groups, chains and policies on this node.
	return h.Proxy.SnapshotIntent(ctx, n.ID, h.Users)
}

// bindUserAccessInbound creates the feature's inbound, or updates the one that
// already owns the listener. The inbound stays enabled even while no user has
// access: the renderer disables a credential-less inbound on its own and
// re-enables it when credentials come back, so re-enabling a user restores
// access without editing the business data.
func (h *Handler) bindUserAccessInbound(ctx context.Context, input UserAccessInput, userIDs []string) error {
	existing, err := h.Proxy.InboundAtListener(ctx, input.NodeID, input.Protocol, input.Listen, input.Port)
	if errors.Is(err, proxy.ErrNotFound) {
		if err := userAccessInboundInput(input, userIDs); err != nil {
			return err
		}
		_, err := h.Proxy.SaveInbound(ctx, "", proxy.InboundInput{Name: userAccessInboundName(input), NodeID: input.NodeID, Protocol: input.Protocol, Listen: input.Listen, Port: input.Port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", AllowedUserIDs: userIDs})
		return err
	}
	if err != nil {
		return err
	}
	if existing.InternalRelay {
		return errors.New("listener belongs to an internal relay inbound")
	}
	// A reuse keeps the Owner's routing and visibility: rerouting a published
	// inbound to Direct only because its users were re-published would change
	// how their traffic leaves the node without an Owner review.
	_, err = h.Proxy.SaveInbound(ctx, existing.ID, proxy.InboundInput{Core: existing.Core, Name: existing.Name, NodeID: existing.NodeID, Protocol: existing.Protocol, Listen: existing.Listen, Port: existing.Port, Enabled: true, SubscriptionVisible: existing.SubscriptionVisible, DefaultOutbound: existing.DefaultOutbound, AllowedUserIDs: mergeUserIDs(existing.AllowedUserIDs, userIDs), Settings: existing.Settings})
	return err
}

func userAccessInboundName(input UserAccessInput) string { return "User access " + input.Protocol }

// userAccessInboundInput runs the proxy module's own inbound validation on the
// inbound the feature is about to create, so a bad listener or port fails before
// anything is written.
func userAccessInboundInput(input UserAccessInput, userIDs []string) error {
	return proxy.ValidateInbound(proxy.InboundInput{Name: userAccessInboundName(input), NodeID: input.NodeID, Protocol: input.Protocol, Listen: input.Listen, Port: input.Port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", AllowedUserIDs: userIDs})
}

// mergeUserIDs unions a stored binding with the users selected now. An empty
// stored list already means "every granted user", so it stays empty instead of
// narrowing to today's selection.
func mergeUserIDs(existing, selected []string) []string {
	if len(existing) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	merged := []string{}
	for _, id := range append(append([]string{}, existing...), selected...) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		merged = append(merged, id)
	}
	return merged
}
