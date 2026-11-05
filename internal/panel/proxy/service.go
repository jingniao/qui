package proxy

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"strconv"
	"strings"
	"sync"
)

type Nodes interface {
	Get(context.Context, string) (node.Node, error)
	SubscriptionGroups(context.Context, string) ([]string, error)
}
type Service struct {
	canvasMu   sync.Mutex
	Repository *Repository
	Nodes      Nodes
	Policies   interface {
		ForNode(context.Context, string) ([]desiredstate.Policy, error)
	}
	// Deploy publishes a canvas drawing through the Deployment module's own
	// batches. It is wired by the panel, because Deployment imports this package
	// for snapshots and cannot be imported back.
	Deploy CanvasDeployer
}

func mergeSettings(incoming, old desiredstate.ProtocolSettings) desiredstate.ProtocolSettings {
	if incoming.Security == "tls" {
		if incoming.CertificatePEM == "" {
			incoming.CertificatePEM = old.CertificatePEM
		}
		if incoming.PrivateKeyPEM == "" {
			incoming.PrivateKeyPEM = old.PrivateKeyPEM
		}
		if incoming.TrustedCAPEM == "" {
			incoming.TrustedCAPEM = old.TrustedCAPEM
		}
	}
	if incoming.WireGuard != nil && incoming.WireGuard.SecretKey == "" && old.WireGuard != nil {
		incoming.WireGuard.SecretKey = old.WireGuard.SecretKey
	}
	// A GOST tunnel's relay halves are written by the canvas, so an Owner edit of
	// the generated inbound keeps them; a nil hop list means "unchanged" and an
	// explicit empty one clears the chain.
	if incoming.Forward != nil && old.Forward != nil {
		forward := *incoming.Forward
		if forward.Mode == "" && old.Forward.Mode != "" {
			forward.Mode, forward.Relay = old.Forward.Mode, old.Forward.Relay
		}
		if forward.Hops == nil && len(old.Forward.Hops) > 0 {
			forward.Hops = old.Forward.Hops
		}
		incoming.Forward = &forward
	}
	return incoming
}
func publicKey(secret string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", errors.New("invalid WireGuard secret")
	}
	key, err := ecdh.X25519().NewPrivateKey(data)
	if err != nil {
		return "", errors.New("invalid WireGuard secret")
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}
func (s *Service) SaveInbound(ctx context.Context, id string, input InboundInput) (Inbound, error) {
	if err := s.Repository.refuseManagedInbound(ctx, id); err != nil {
		return Inbound{}, err
	}
	return s.writeInbound(ctx, id, input, false)
}

// saveGeneratedInbound is the canvas path: it may set the GOST relay halves of a
// tunnel, which the public inbound API never accepts.
func (s *Service) saveGeneratedInbound(ctx context.Context, id string, input InboundInput) (Inbound, error) {
	return s.writeInbound(ctx, id, input, true)
}

// sameRelayMaterial reports whether two records carry the same tunnel material.
// The private halves are masked first, because the API never returns them: an
// Owner save that echoes the stored record is not a change.
func sameRelayMaterial(incoming, stored *desiredstate.ForwardSettings) bool {
	material := func(forward *desiredstate.ForwardSettings) string {
		if forward == nil {
			return ""
		}
		masked := mask(desiredstate.ProtocolSettings{Forward: forward}).Forward
		relay, _ := json.Marshal(masked.Relay)
		hops, _ := json.Marshal(masked.Hops)
		return masked.Mode + string(relay) + string(hops)
	}
	return material(incoming) == material(stored)
}

func (s *Service) writeInbound(ctx context.Context, id string, input InboundInput, generated bool) (Inbound, error) {
	n, err := s.Nodes.Get(ctx, input.NodeID)
	if err != nil || !n.Enabled || n.DeletedAt != "" {
		return Inbound{}, errors.New("node missing or disabled")
	}
	if node.CoreSelectionIsBlocked(n, input.Core) {
		return Inbound{}, errors.New("core_pending: wait for success and refreshed capabilities")
	}
	if err := ValidateInboundCore(n.Capabilities, input.Core, input.Protocol); err != nil {
		return Inbound{}, err
	}
	if err := ValidateInboundForwardOptions(n.Capabilities, input.Core, input.Settings); err != nil {
		return Inbound{}, err
	}
	// The same rule desiredstate.Validate applies at publication, reported here
	// at save time.
	if input.Core == desiredstate.CoreGost && input.DefaultOutbound != "direct" {
		return Inbound{}, errors.New("GOST 转发只能直接出站（默认出站必须是 direct）")
	}
	if id != "" {
		old, err := s.Repository.Inbound(ctx, id, true)
		if err != nil {
			return Inbound{}, err
		}
		// A drawing owns its objects by identity, not by name: an ordinary rename
		// here would detach the inbound from the canvas, which knows it under the
		// name it generated. The canvas may still rename it (adoption).
		if !generated && input.Name != old.Name {
			owner, err := s.Repository.objectOwner(ctx, "inbound", id)
			if err != nil {
				return Inbound{}, err
			}
			if owner != "" {
				return Inbound{}, errors.New("该入站由画布隧道管理，不能在入站编辑器中修改名称。")
			}
		}
		// The relay halves of the inbound a GOST tunnel generated belong to the
		// tunnel: an ordinary save keeps the stored material and refuses a record
		// that brings different material of its own. The merged record is written
		// with the generated-inbound rules, because it still carries them.
		if stored := old.Settings.Forward; !generated && stored.HasRelaySettings() {
			generated = true
			if input.Settings.Forward.HasRelaySettings() && !sameRelayMaterial(input.Settings.Forward, stored) {
				return Inbound{}, errors.New("GOST 中继证书由隧道资源管理，不能在入站中填写")
			}
			if input.Settings.Forward != nil {
				input.Settings.Forward.Mode, input.Settings.Forward.Relay, input.Settings.Forward.Hops = "", nil, nil
			}
		}
		input.Settings = mergeSettings(input.Settings, old.Settings)
	}
	// The public shape never carries a GOST tunnel's relay halves: they belong to
	// the tunnel resource and are written only by the canvas.
	if !generated && input.Settings.Forward.HasRelaySettings() {
		return Inbound{}, errors.New("GOST 中继证书由隧道资源管理，不能在入站中填写")
	}
	if input.InternalRelay {
		input.SubscriptionVisible = false
		input.AllowedUserIDs = []string{}
	}
	if input.Protocol == "trojan" || input.Protocol == "hysteria2" || input.Protocol == "tuic" {
		input.Settings.Security = "tls"
	}
	if input.InternalRelay && input.Settings.Security == "tls" && input.Settings.PrivateKeyPEM == "" {
		serverName := n.PublicIP
		if serverName == "" {
			serverName = n.Hostname
		}
		if serverName == "" {
			return Inbound{}, errors.New("relay TLS needs node IP or hostname")
		}
		managed, err := managedTLS(serverName)
		if err != nil {
			return Inbound{}, err
		}
		managed.Transport = input.Settings.Transport
		managed.Path = input.Settings.Path
		managed.ServiceName = input.Settings.ServiceName
		managed.QUIC = input.Settings.QUIC
		input.Settings = managed
	}
	if input.Protocol == "wireguard" {
		if input.Settings.WireGuard == nil {
			input.Settings.WireGuard = &desiredstate.WireGuardSettings{Addresses: []string{"10.77.0.1/24"}, Peers: []desiredstate.WireGuardPeer{}}
		}
		if input.Settings.WireGuard.SecretKey == "" {
			secret, err := user.GenerateCredential("wireguard")
			if err != nil {
				return Inbound{}, err
			}
			input.Settings.WireGuard.SecretKey = secret.Value
		}
	}
	if strings.HasPrefix(input.DefaultOutbound, "group:") {
		group, err := s.Repository.Group(ctx, strings.TrimPrefix(input.DefaultOutbound, "group:"))
		if err != nil || !group.Enabled || group.NodeID != input.NodeID {
			return Inbound{}, errors.New("default outbound group unavailable")
		}
	} else if strings.HasPrefix(input.DefaultOutbound, "chain:") {
		chain, err := s.Repository.Chain(ctx, strings.TrimPrefix(input.DefaultOutbound, "chain:"))
		if err != nil || !chain.Enabled || chain.NodeIDs[0] != input.NodeID {
			return Inbound{}, errors.New("default proxy chain unavailable")
		}
	} else if input.DefaultOutbound != "direct" && input.DefaultOutbound != "block" {
		outbound, err := s.Repository.Outbound(ctx, input.DefaultOutbound, false)
		if err != nil || outbound.NodeID != input.NodeID || !outbound.Enabled {
			return Inbound{}, errors.New("default outbound unavailable on this node")
		}
	}
	if generated {
		return s.Repository.SaveGeneratedInbound(ctx, id, input)
	}
	return s.Repository.SaveInbound(ctx, id, input)
}

// InboundAtListener reports the inbound that already owns a node listener, so a
// caller holding business data for that listener can update it rather than
// collide with it. The module boundary stays here: callers never query the
// inbounds table themselves.
func (s *Service) InboundAtListener(ctx context.Context, nodeID, protocol, listen string, port int) (Inbound, error) {
	return s.Repository.InboundAtListener(ctx, nodeID, protocol, listen, port)
}
func (s *Service) SaveOutbound(ctx context.Context, id string, input OutboundInput) (Outbound, error) {
	n, err := s.Nodes.Get(ctx, input.NodeID)
	if err != nil || !n.Enabled || n.DeletedAt != "" {
		return Outbound{}, errors.New("node missing or disabled")
	}
	if err := ValidateOutboundCores(n.Capabilities, input.Protocol); err != nil {
		return Outbound{}, err
	}
	if id != "" {
		old, err := s.Repository.Outbound(ctx, id, true)
		if err != nil {
			return Outbound{}, err
		}
		input.Settings = mergeSettings(input.Settings, old.Settings)
	}
	if input.FailurePolicy == "" {
		input.FailurePolicy = "Block"
	}
	if input.ProbeURL == "" {
		input.ProbeURL = "https://www.gstatic.com/generate_204"
	}
	if input.ProbeInterval == 0 {
		input.ProbeInterval = 30
	}
	if err := scheduler.ValidateProbeURL(input.ProbeURL); err != nil {
		return Outbound{}, err
	}
	if input.ProbeInterval < 3 || input.ProbeInterval > 300 {
		return Outbound{}, errors.New("invalid outbound probe interval")
	}
	if input.Protocol == "direct" || input.Protocol == "block" {
		if input.FailurePolicy != "Block" {
			return Outbound{}, errors.New("Direct and Block outbounds do not have a failure target")
		}
		input.SupportTCP = true
		input.SupportUDP = true
	} else if input.Protocol == "http" {
		input.SupportUDP = false
	}
	if input.TargetInboundID != "" {
		target, err := s.Repository.Inbound(ctx, input.TargetInboundID, true)
		if err != nil || !target.Enabled || !target.InternalRelay || target.NodeID == input.NodeID {
			return Outbound{}, errors.New("target must be an enabled internal inbound on another node")
		}
		if target.Protocol != input.Protocol {
			return Outbound{}, errors.New("relay outbound must match target inbound protocol")
		}
		targetNode, err := s.Nodes.Get(ctx, target.NodeID)
		if err != nil || !targetNode.Enabled || targetNode.DeletedAt != "" || targetNode.PublicIP == "" {
			return Outbound{}, errors.New("target node needs an enabled public endpoint")
		}
		input.TargetNodeID = target.NodeID
		input.Address = targetNode.PublicIP
		input.Port = target.Port
		input.Settings = target.Settings
		input.Settings.PrivateKeyPEM = ""
		input.Settings.CertificatePEM = ""
		input.Secret = ""
		input.Username = ""
		if input.Protocol == "wireguard" {
			serverKey, err := publicKey(target.Settings.WireGuard.SecretKey)
			if err != nil {
				return Outbound{}, err
			}
			secret, err := user.GenerateCredential("wireguard")
			if err != nil {
				return Outbound{}, err
			}
			input.Settings.WireGuard = &desiredstate.WireGuardSettings{SecretKey: secret.Value, Addresses: target.Settings.WireGuard.Addresses, Peers: []desiredstate.WireGuardPeer{{PublicKey: serverKey, Endpoint: net.JoinHostPort(targetNode.PublicIP, strconv.Itoa(target.Port)), AllowedIPs: []string{"0.0.0.0/0", "::/0"}, KeepAlive: 15}}, MTU: target.Settings.WireGuard.MTU}
			if id != "" {
				previous, err := s.Repository.Outbound(ctx, id, true)
				if err != nil {
					return Outbound{}, err
				}
				if previous.Settings.WireGuard != nil {
					input.Settings.WireGuard.SecretKey = previous.Settings.WireGuard.SecretKey
					input.Settings.WireGuard.Addresses = previous.Settings.WireGuard.Addresses
				}
			}
		}
	}
	if input.Enabled && input.TargetNodeID != "" {
		if err := s.checkRelayCycle(ctx, input.NodeID, input.TargetNodeID, id); err != nil {
			return Outbound{}, err
		}
	}
	if input.Protocol == "wireguard" && input.Settings.WireGuard != nil && input.Settings.WireGuard.SecretKey == "" {
		secret, err := user.GenerateCredential("wireguard")
		if err != nil {
			return Outbound{}, err
		}
		input.Settings.WireGuard.SecretKey = secret.Value
	}
	if err := s.validateBackup(ctx, input.NodeID, input.FailurePolicy, input.BackupOutboundID, input.SupportTCP, input.SupportUDP); err != nil {
		return Outbound{}, err
	}
	candidateID := id
	if candidateID == "" {
		candidateID = "_new_outbound_"
	}
	if err := s.checkSchedulingChange(ctx, input.NodeID, candidateID, input); err != nil {
		return Outbound{}, err
	}
	if (input.Protocol == "vless" || input.Protocol == "trojan" || input.Protocol == "shadowsocks" || input.Protocol == "hysteria2" || input.Protocol == "tuic") && input.TargetInboundID == "" && input.Secret == "" && id == "" {
		return Outbound{}, errors.New("outbound credential required")
	}
	if input.Protocol == "tuic" && input.TargetInboundID == "" && input.Secret != "" {
		if err := desiredstate.ValidateUUID(input.Username); err != nil {
			return Outbound{}, err
		}
	}
	if len(input.Secret) > 512 || len(input.Username) > 128 {
		return Outbound{}, errors.New("outbound credential exceeds limit")
	}
	return s.Repository.SaveOutbound(ctx, id, input)
}
