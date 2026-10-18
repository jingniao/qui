package deployment

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
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
	if h.Users == nil {
		httpapi.Error(w, 503, "unavailable", "User access service unavailable")
		return
	}
	state, err := h.UserState(r.Context(), input)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	revision, err := h.Repository.Create(r.Context(), state, auth.Actor(r.Context()).Username, input.Summary)
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
func (h *Handler) UserState(ctx context.Context, input UserAccessInput) (desiredstate.State, error) {
	if strings.TrimSpace(input.Summary) == "" || len(input.Summary) > 512 {
		return desiredstate.State{}, errors.New("revision summary required")
	}
	if input.Protocol != "http" && input.Protocol != "socks5" {
		return desiredstate.State{}, errors.New("user access bootstrap supports HTTP and SOCKS5")
	}
	n, err := h.Nodes.Get(ctx, input.NodeID)
	if err != nil || !n.Enabled {
		return desiredstate.State{}, errors.New("target node missing or disabled")
	}
	groups, err := h.Nodes.SubscriptionGroups(ctx, n.ID)
	if err != nil {
		return desiredstate.State{}, err
	}
	access, err := h.Users.AccessForGroups(ctx, groups)
	if err != nil {
		return desiredstate.State{}, err
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{}, Credentials: []desiredstate.Credential{}, Inbounds: []desiredstate.Inbound{}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}, OutboundGroups: []desiredstate.OutboundGroup{}, ProxyChains: []desiredstate.ProxyChain{}, Policies: []desiredstate.Policy{}}
	// The node's outbound network settings travel with every published state;
	// a node that configures none keeps the field absent.
	state.Network = desiredstate.NetworkOrNil(n.OutboundSourceIP, n.OutboundInterface, n.HostSourceIPv4, n.HostSourceIPv6)
	inbound := desiredstate.Inbound{ID: "user-inbound", Protocol: input.Protocol, Listen: input.Listen, Port: input.Port, DefaultOutbound: "direct", SubscriptionVisible: true, CredentialIDs: []string{}}
	for _, entry := range access {
		state.Users = append(state.Users, desiredstate.User{ID: entry.User.ID, Enabled: true, ExpiresAt: entry.User.ExpiresAt})
		for _, credential := range entry.Credentials {
			if credential.Protocol != input.Protocol {
				continue
			}
			state.Credentials = append(state.Credentials, desiredstate.Credential{ID: credential.ID, UserID: credential.UserID, DeviceID: credential.DeviceID, Protocol: credential.Protocol, Username: credential.Username, Secret: credential.Secret})
			inbound.CredentialIDs = append(inbound.CredentialIDs, credential.ID)
		}
	}
	inbound.Enabled = len(inbound.CredentialIDs) > 0
	state.Inbounds = append(state.Inbounds, inbound)
	if h.Policies != nil {
		rules, err := h.Policies.ForNode(ctx, input.NodeID)
		if err != nil {
			return state, err
		}
		state.Policies = rules
	}
	return state, state.Validate()
}
