package subscription

import (
	"context"
	"crypto/ecdh"
	"database/sql"
	"encoding/base64"
	"errors"
	"net"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

type Users interface {
	Get(context.Context, string) (user.User, error)
	Device(context.Context, string) (user.Device, error)
	Credentials(context.Context, string, bool) ([]user.Credential, error)
}
type Nodes interface {
	ExpandedNodes(context.Context, []string) ([]node.Node, error)
}
type Published interface {
	Published(context.Context, string) (desiredstate.State, error)
}
type Service struct {
	Repository *Repository
	Users      Users
	Nodes      Nodes
	Published  Published
}
type Endpoint struct {
	NodeID             string                        `json:"nodeId"`
	NodeName           string                        `json:"nodeName"`
	InboundID          string                        `json:"inboundId"`
	Protocol           string                        `json:"protocol"`
	Address            string                        `json:"address"`
	Port               int                           `json:"port"`
	Credential         user.Credential               `json:"-"`
	Settings           desiredstate.ProtocolSettings `json:"settings"`
	WireGuardAddress   string                        `json:"wireGuardAddress,omitempty"`
	WireGuardAddresses []string                      `json:"wireGuardAddresses,omitempty"`
	WireGuardPublicKey string                        `json:"wireGuardPublicKey,omitempty"`
}

func (s *Service) Rotate(ctx context.Context, userID, deviceID string) (Token, string, error) {
	u, err := s.Users.Get(ctx, userID)
	if err != nil {
		return Token{}, "", err
	}
	if !u.Active(time.Now()) {
		return Token{}, "", user.ErrInactive
	}
	device, err := s.Users.Device(ctx, deviceID)
	if err != nil {
		return Token{}, "", err
	}
	if device.UserID != userID || device.Status != "active" {
		return Token{}, "", user.ErrCredential
	}
	return s.Repository.Rotate(ctx, userID, deviceID)
}
func (s *Service) Endpoints(ctx context.Context, userID, deviceID string) ([]Endpoint, error) {
	u, err := s.Users.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !u.Active(time.Now()) {
		return nil, user.ErrInactive
	}
	device, err := s.Users.Device(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.UserID != userID || device.Status != "active" {
		return nil, user.ErrCredential
	}
	credentials, err := s.Users.Credentials(ctx, deviceID, true)
	if err != nil {
		return nil, err
	}
	active := map[string]user.Credential{}
	allowed := map[string]bool{}
	for _, protocol := range u.AllowedProtocols {
		allowed[protocol] = true
	}
	for _, credential := range credentials {
		if credential.Status == "active" && allowed[credential.Protocol] {
			active[credential.ID] = credential
		}
	}
	nodes, err := s.Nodes.ExpandedNodes(ctx, u.NodeGroupIDs)
	if err != nil {
		return nil, err
	}
	endpoints := []Endpoint{}
	for _, n := range nodes {
		state, err := s.Published.Published(ctx, n.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if state.NodeID != n.ID {
			return nil, errors.New("published state identity mismatch")
		}
		state, err = state.RuntimeAt(time.Now().UTC())
		if err != nil {
			return nil, err
		}
		for _, inbound := range state.Inbounds {
			if !inbound.Enabled || !inbound.SubscriptionVisible || !allowed[inbound.Protocol] {
				continue
			}
			if listen := net.ParseIP(inbound.Listen); listen == nil || listen.IsLoopback() {
				continue
			}
			supported := false
			for _, capability := range n.Capabilities.Protocols {
				if capability.Protocol == inbound.Protocol && capability.Inbound {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
			for _, credentialID := range inbound.CredentialIDs {
				credential, ok := active[credentialID]
				if !ok || credential.Protocol != inbound.Protocol {
					continue
				}
				publishedValid := false
				for _, publishedCredential := range state.Credentials {
					if publishedCredential.ID == credential.ID && !publishedCredential.Internal && publishedCredential.UserID == userID && publishedCredential.DeviceID == deviceID && publishedCredential.Username == credential.Username && (publishedCredential.Secret == credential.Secret || credential.Protocol == "wireguard" && publishedCredential.PublicKey == credential.PublicKey) {
						publishedValid = true
						break
					}
				}
				if !publishedValid {
					continue
				}
				if n.PublicIP == "" {
					continue
				}
				settings := inbound.Settings
				wgAddress, wgPublic := "", ""
				var wgAddresses []string
				if settings.WireGuard != nil {
					raw, err := base64.StdEncoding.DecodeString(settings.WireGuard.SecretKey)
					if err != nil {
						return nil, errors.New("published WireGuard key invalid")
					}
					key, err := ecdh.X25519().NewPrivateKey(raw)
					clear(raw)
					if err != nil {
						return nil, errors.New("published WireGuard key invalid")
					}
					wgPublic = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
					for _, peer := range settings.WireGuard.Peers {
						if peer.CredentialID == credential.ID && len(peer.AllowedIPs) > 0 {
							if len(wgAddresses) > 0 || peer.PublicKey != credential.PublicKey {
								return nil, errors.New("published WireGuard peer identity conflict")
							}
							wgAddress = peer.AllowedIPs[0]
							wgAddresses = append([]string(nil), peer.AllowedIPs...)
						}
					}
					if wgAddress == "" {
						continue
					}
				}
				settings.PrivateKeyPEM = ""
				settings.CertificatePEM = ""
				if settings.WireGuard != nil {
					copy := *settings.WireGuard
					copy.SecretKey = ""
					settings.WireGuard = &copy
				}
				endpoint := Endpoint{NodeID: n.ID, NodeName: n.Name, InboundID: inbound.ID, Protocol: inbound.Protocol, Address: n.PublicIP, Port: inbound.Port, Credential: credential, Settings: settings, WireGuardAddress: wgAddress, WireGuardAddresses: wgAddresses, WireGuardPublicKey: wgPublic}
				if inbound.Protocol == "wireguard" {
					if _, err := wireguardEndpointAddresses(endpoint); err != nil {
						return nil, err
					}
				}
				endpoints = append(endpoints, endpoint)
				break
			}
		}
	}
	return endpoints, nil
}

// fetchMarkTimeout bounds the last-fetched marker. The marker is informational,
// so a busy writer (a long maintenance transaction, for example) must not delay
// or fail the subscription itself.
const fetchMarkTimeout = 250 * time.Millisecond

func (s *Service) Content(ctx context.Context, plaintext, format string) ([]byte, Token, error) {
	token, err := s.Repository.Resolve(ctx, plaintext)
	if err != nil {
		return nil, token, err
	}
	endpoints, err := s.Endpoints(ctx, token.UserID, token.DeviceID)
	if err != nil {
		return nil, token, err
	}
	content, err := Export(endpoints, format)
	if err != nil {
		return nil, token, err
	}
	markCtx, cancel := context.WithTimeout(ctx, fetchMarkTimeout)
	defer cancel()
	// Best effort: the subscription is already rendered, and the marker only
	// feeds the last-fetched column of the token list.
	_ = s.Repository.Fetched(markCtx, token.ID)
	return content, token, nil
}
