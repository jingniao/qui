// Package published is the panel's primary subscription source: the endpoints a
// user's own nodes have actually published to them.
//
// A credential only appears once the node's last successful revision contains it,
// so a subscription never advertises an endpoint the node would refuse.
package published

import (
	"context"
	"crypto/ecdh"
	"database/sql"
	"encoding/base64"
	"errors"
	"net"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Users reads the account's credentials. The account itself is validated by the
// subscription service before a source is asked anything.
type Users interface {
	Credentials(ctx context.Context, deviceID string, activeOnly bool) ([]user.Credential, error)
}

// Nodes expands the granted node groups.
type Nodes interface {
	ExpandedNodes(ctx context.Context, groupIDs []string) ([]node.Node, error)
	UserInboundGroups(ctx context.Context, inboundID, userID string) ([]string, error)
}

// Published reads a node's last successfully applied state.
type Published interface {
	Published(ctx context.Context, nodeID string) (desiredstate.State, error)
}

// Source is the "published" source.
type Source struct {
	Users     Users
	Nodes     Nodes
	Published Published
}

// ID names the source in warnings, logs and origin fields.
func (s *Source) ID() string { return "published" }

// Optional reports that this source is primary: its errors stay fatal, because an
// empty but successful subscription would make every client wipe its servers.
func (s *Source) Optional() bool { return false }

// Proxies returns one client proxy per subscription-visible inbound of the
// granted nodes for which the published revision holds this account's credential.
func (s *Source) Proxies(ctx context.Context, viewer source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	credentials, err := s.Users.Credentials(ctx, viewer.DeviceID, true)
	if err != nil {
		return nil, nil, err
	}
	active := map[string]user.Credential{}
	for _, credential := range credentials {
		if credential.Status == "active" && viewer.AllowsProtocol(credential.Protocol) {
			active[credential.ID] = credential
		}
	}
	nodes, err := s.Nodes.ExpandedNodes(ctx, viewer.NodeGroupIDs)
	if err != nil {
		return nil, nil, err
	}
	endpoints := []proxy.Proxy{}
	for _, n := range nodes {
		state, err := s.Published.Published(ctx, n.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if state.NodeID != n.ID {
			return nil, nil, errors.New("published state identity mismatch")
		}
		state, err = state.RuntimeAt(time.Now().UTC())
		if err != nil {
			return nil, nil, err
		}
		for _, inbound := range state.Inbounds {
			labels, err := s.labels(ctx, inbound.ID, viewer)
			if err != nil {
				return nil, nil, err
			}
			if len(labels) == 0 {
				continue
			}
			if inbound.Protocol == "forward" || !inbound.Enabled || !inbound.SubscriptionVisible || !viewer.AllowsProtocol(inbound.Protocol) {
				continue
			}
			if listen := net.ParseIP(inbound.Listen); listen == nil || listen.IsLoopback() {
				continue
			}
			if !supportsInbound(n, inbound.Protocol) {
				continue
			}
			for _, credentialID := range inbound.CredentialIDs {
				credential, ok := active[credentialID]
				if !ok || credential.Protocol != inbound.Protocol {
					continue
				}
				if !publishedCredential(state, credential, viewer) {
					continue
				}
				if n.PublicIP == "" {
					continue
				}
				endpoint, ok, err := build(n, inbound, credential, labels)
				if err != nil {
					return nil, nil, err
				}
				if !ok {
					continue
				}
				endpoints = append(endpoints, endpoint)
				break
			}
		}
	}
	return endpoints, nil, nil
}

// labels reports the node groups this account was granted that contain the node.
// Only granted groups are labelled: a rule template expands @nodeGroup with them,
// and it must never expand to a group the account cannot use.
func (s *Source) labels(ctx context.Context, nodeID string, viewer source.Viewer) ([]string, error) {
	groups, err := s.Nodes.UserInboundGroups(ctx, nodeID, viewer.UserID)
	if err != nil {
		return nil, err
	}
	labels := []string{}
	for _, groupID := range groups {
		if viewer.GrantsGroup(groupID) {
			labels = append(labels, "nodeGroup:"+groupID)
		}
	}
	return labels, nil
}

func supportsInbound(n node.Node, protocol string) bool {
	if n.Capabilities == nil {
		return false
	}
	for _, capability := range n.Capabilities.Protocols {
		if capability.Protocol == protocol && capability.Inbound {
			return true
		}
	}
	return false
}

// publishedCredential reports whether the revision the node is running holds this
// exact credential. A rotated credential is therefore withheld until the node has
// the new one.
func publishedCredential(state desiredstate.State, credential user.Credential, viewer source.Viewer) bool {
	for _, published := range state.Credentials {
		if published.ID == credential.ID && !published.Internal && published.UserID == viewer.UserID && published.DeviceID == viewer.DeviceID && published.Username == credential.Username && (published.Secret == credential.Secret || credential.Protocol == "wireguard" && published.PublicKey == credential.PublicKey) {
			return true
		}
	}
	return false
}

func build(n node.Node, inbound desiredstate.Inbound, credential user.Credential, labels []string) (proxy.Proxy, bool, error) {
	settings := inbound.Settings
	endpoint := proxy.Proxy{
		Origin:     proxy.Origin{Source: "published", Ref: inbound.ID, Node: proxy.Ref{ID: n.ID, Name: n.Name}, Labels: labels},
		Name:       n.Name + " · " + inbound.Protocol,
		Server:     n.PublicIP,
		Port:       inbound.Port,
		Protocol:   inbound.Protocol,
		Credential: proxy.Credential{Username: credential.Username, Password: credential.Secret, PublicKey: credential.PublicKey},
		Flow:       settings.Flow,
		Method:     settings.Method,
		Security:   settings.Security,
	}
	if settings.Security != "" || settings.ServerName != "" || settings.TrustedCAPEM != "" {
		endpoint.TLS = &proxy.TLS{ServerName: settings.ServerName, PinnedCA: settings.TrustedCAPEM}
	}
	if settings.Transport != "" {
		endpoint.Transport = &proxy.Transport{Type: settings.Transport, Path: settings.Path, ServiceName: settings.ServiceName}
	}
	if settings.QUIC != nil {
		endpoint.QUIC = &proxy.QUIC{UpMbps: settings.QUIC.UpMbps, DownMbps: settings.QUIC.DownMbps, CongestionControl: settings.QUIC.CongestionControl, UDPRelayMode: settings.QUIC.UDPRelayMode}
	}
	if settings.WireGuard != nil {
		peer, ok, err := wireguardPeer(settings.WireGuard, credential)
		if err != nil || !ok {
			return proxy.Proxy{}, false, err
		}
		endpoint.WireGuard = peer
		// The client identity is validated here, not only in the exporter, so a
		// broken key pair fails the subscription instead of producing a config the
		// client cannot use.
		if _, err := endpoint.WireGuardAddresses(); err != nil {
			return proxy.Proxy{}, false, err
		}
	}
	return endpoint, true, nil
}

// wireguardPeer derives the server public key and finds this credential's address
// inside the published WireGuard inbound.
func wireguardPeer(settings *desiredstate.WireGuardSettings, credential user.Credential) (*proxy.WireGuard, bool, error) {
	raw, err := base64.StdEncoding.DecodeString(settings.SecretKey)
	if err != nil {
		return nil, false, errors.New("published WireGuard key invalid")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	clear(raw)
	if err != nil {
		return nil, false, errors.New("published WireGuard key invalid")
	}
	peer := &proxy.WireGuard{ServerPublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), MTU: settings.MTU}
	for _, candidate := range settings.Peers {
		if candidate.CredentialID != credential.ID || len(candidate.AllowedIPs) == 0 {
			continue
		}
		if len(peer.Addresses) > 0 || candidate.PublicKey != credential.PublicKey {
			return nil, false, errors.New("published WireGuard peer identity conflict")
		}
		peer.Addresses = append([]string(nil), candidate.AllowedIPs...)
	}
	if len(peer.Addresses) == 0 {
		// A peer the node does not know is not an endpoint for this client.
		return nil, false, nil
	}
	return peer, true, nil
}
