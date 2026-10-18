package proxy

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"strconv"
	"strings"
)

type Nodes interface {
	Get(context.Context, string) (node.Node, error)
	SubscriptionGroups(context.Context, string) ([]string, error)
}
type Service struct {
	Repository *Repository
	Nodes      Nodes
	Policies   interface {
		ForNode(context.Context, string) ([]desiredstate.Policy, error)
	}
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
	n, err := s.Nodes.Get(ctx, input.NodeID)
	if err != nil || !n.Enabled {
		return Inbound{}, errors.New("node missing or disabled")
	}
	if err := ValidateCoreProtocol(n.Capabilities.GetCoreName(), input.Protocol, true); err != nil {
		return Inbound{}, err
	}
	if id != "" {
		old, err := s.Repository.Inbound(ctx, id, true)
		if err != nil {
			return Inbound{}, err
		}
		input.Settings = mergeSettings(input.Settings, old.Settings)
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
		generated, err := managedTLS(serverName)
		if err != nil {
			return Inbound{}, err
		}
		generated.Transport = input.Settings.Transport
		generated.Path = input.Settings.Path
		generated.ServiceName = input.Settings.ServiceName
		generated.QUIC = input.Settings.QUIC
		input.Settings = generated
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
	return s.Repository.SaveInbound(ctx, id, input)
}
func (s *Service) SaveOutbound(ctx context.Context, id string, input OutboundInput) (Outbound, error) {
	n, err := s.Nodes.Get(ctx, input.NodeID)
	if err != nil || !n.Enabled {
		return Outbound{}, errors.New("node missing or disabled")
	}
	if err := ValidateCoreProtocol(n.Capabilities.GetCoreName(), input.Protocol, false); err != nil {
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
		if err != nil || !targetNode.Enabled || targetNode.PublicIP == "" {
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
