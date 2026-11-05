package proxy

import (
	"errors"
	"fmt"
	"net"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"strings"
)

var ErrNotFound = errors.New("proxy configuration not found")

type Inbound struct {
	ManagedBy            *PortOwner                    `json:"managedBy,omitempty"`
	UserTrafficSupported *bool                         `json:"userTrafficSupported"`
	DeviceGateSupported  *bool                         `json:"deviceGateSupported"`
	DeviceGateCoverage   *string                       `json:"deviceGateCoverage"`
	Core                 string                        `json:"core"`
	ID                   string                        `json:"id"`
	Name                 string                        `json:"name"`
	NodeID               string                        `json:"nodeId"`
	Protocol             string                        `json:"protocol"`
	Listen               string                        `json:"listen"`
	Port                 int                           `json:"port"`
	Enabled              bool                          `json:"enabled"`
	SubscriptionVisible  bool                          `json:"subscriptionVisible"`
	InternalRelay        bool                          `json:"internalRelay"`
	DefaultOutbound      string                        `json:"defaultOutbound"`
	AllowedUserIDs       []string                      `json:"allowedUserIds"`
	Settings             desiredstate.ProtocolSettings `json:"settings"`
	RelayCredentialID    string                        `json:"relayCredentialId,omitempty"`
	CreatedAt            string                        `json:"createdAt"`
	HasTLSKey            bool                          `json:"hasTlsKey"`
	HasWireGuardKey      bool                          `json:"hasWireGuardKey"`
	HasRealityKey        bool                          `json:"hasRealityKey"`
}
type InboundInput struct {
	Core                string                        `json:"core"`
	Name                string                        `json:"name"`
	NodeID              string                        `json:"nodeId"`
	Protocol            string                        `json:"protocol"`
	Listen              string                        `json:"listen"`
	Port                int                           `json:"port"`
	Enabled             bool                          `json:"enabled"`
	SubscriptionVisible bool                          `json:"subscriptionVisible"`
	InternalRelay       bool                          `json:"internalRelay"`
	DefaultOutbound     string                        `json:"defaultOutbound"`
	AllowedUserIDs      []string                      `json:"allowedUserIds"`
	Settings            desiredstate.ProtocolSettings `json:"settings"`
}
type Outbound struct {
	ID                 string                        `json:"id"`
	Name               string                        `json:"name"`
	NodeID             string                        `json:"nodeId"`
	Protocol           string                        `json:"protocol"`
	Address            string                        `json:"address"`
	Port               int                           `json:"port"`
	Enabled            bool                          `json:"enabled"`
	TargetNodeID       string                        `json:"targetNodeId"`
	TargetInboundID    string                        `json:"targetInboundId"`
	CredentialID       string                        `json:"credentialId,omitempty"`
	Settings           desiredstate.ProtocolSettings `json:"settings"`
	SupportTCP         bool                          `json:"supportTcp"`
	SupportUDP         bool                          `json:"supportUdp"`
	FailurePolicy      string                        `json:"failurePolicy"`
	BackupOutboundID   string                        `json:"backupOutboundId"`
	CreatedAt          string                        `json:"createdAt"`
	HasCredential      bool                          `json:"hasCredential"`
	HasWireGuardKey    bool                          `json:"hasWireGuardKey"`
	WireGuardPublicKey string                        `json:"wireGuardPublicKey,omitempty"`
	ProbeURL           string                        `json:"probeUrl"`
	ProbeInterval      int                           `json:"probeInterval"`
	Health             *scheduler.Health             `json:"health,omitempty"`
}
type OutboundInput struct {
	Name             string                        `json:"name"`
	NodeID           string                        `json:"nodeId"`
	Protocol         string                        `json:"protocol"`
	Address          string                        `json:"address"`
	Port             int                           `json:"port"`
	Enabled          bool                          `json:"enabled"`
	TargetNodeID     string                        `json:"targetNodeId"`
	TargetInboundID  string                        `json:"targetInboundId"`
	Username         string                        `json:"username"`
	Secret           string                        `json:"secret"`
	Settings         desiredstate.ProtocolSettings `json:"settings"`
	SupportTCP       bool                          `json:"supportTcp"`
	SupportUDP       bool                          `json:"supportUdp"`
	FailurePolicy    string                        `json:"failurePolicy"`
	BackupOutboundID string                        `json:"backupOutboundId"`
	ProbeURL         string                        `json:"probeUrl"`
	ProbeInterval    int                           `json:"probeInterval"`
}

func protocol(value string, outbound bool) bool {
	if outbound {
		return desiredstate.SupportsOutbound(value)
	}
	return desiredstate.SupportsInbound(value)
}

// ValidateCoreProtocol rejects a protocol the node's own core cannot carry,
// using the same per-core basis the API and the UI read
// (desiredstate.CoreSupport, docs/protocol-support-matrix.md). A core the
// matrix does not describe - including a node that has not reported one yet -
// is left to the node's runtime capability report, which the Agent enforces
// when it renders the configuration. A described core that is absent from a
// protocol's row cannot carry it: GOST is forward-only, so every other row
// refuses it here instead of falling through to the unknown-core branch.
func ValidateCoreProtocol(core, protocol string, inbound bool) error {
	support, ok := desiredstate.CoreSupportFor(core, protocol)
	if !ok {
		if desiredstate.KnownCore(core) && (desiredstate.SupportsInbound(protocol) || desiredstate.SupportsOutbound(protocol)) {
			return fmt.Errorf("protocol %s is not supported by the node's %s core", protocol, core)
		}
		return nil
	}
	if inbound && !support.Inbound {
		return fmt.Errorf("protocol %s is not supported by the node's %s core", protocol, core)
	}
	if !inbound && !support.Outbound {
		return fmt.Errorf("protocol %s is not supported by the node's %s core", protocol, core)
	}
	return nil
}

// ValidateInbound validates an inbound the Owner configured.
func ValidateInbound(input InboundInput) error { return validateInboundInput(input, false) }

// validateGeneratedInbound is the canvas rule: the same shape plus the relay
// halves of a GOST tunnel, which the public API refuses.
func validateGeneratedInbound(input InboundInput) error { return validateInboundInput(input, true) }

func validateInboundInput(input InboundInput, generated bool) error {
	// The public shape never carries a GOST tunnel's relay halves: they belong to
	// the tunnel resource and are written only by the canvas.
	if !generated && input.Settings.Forward.HasRelaySettings() {
		return errors.New("GOST 中继证书由隧道资源管理，不能在入站中填写")
	}
	if input.Protocol == "forward" && (input.SubscriptionVisible || input.InternalRelay || len(input.AllowedUserIDs) > 0) {
		return errors.New("forward inbounds have no user credentials and must be hidden from subscriptions")
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 120 || input.NodeID == "" || !protocol(input.Protocol, false) || net.ParseIP(input.Listen) == nil || input.Port < 1 || input.Port > 65535 {
		return errors.New("invalid inbound name, node, protocol or listener")
	}
	if input.DefaultOutbound == "" {
		return errors.New("default outbound required")
	}
	if len(input.AllowedUserIDs) > 100 {
		return errors.New("too many allowed users")
	}
	seen := map[string]bool{}
	for _, id := range input.AllowedUserIDs {
		if seen[id] || id == "" {
			return errors.New("invalid allowed user list")
		}
		seen[id] = true
	}
	if input.InternalRelay && input.SubscriptionVisible {
		return errors.New("internal relay must be hidden from subscriptions")
	}
	if input.Protocol == "wireguard" {
		if _, _, err := wireguardPools(input.Settings); err != nil {
			return err
		}
	}
	return desiredstate.ValidateSettings(input.Protocol, input.Settings, true)
}
func ValidateOutbound(input OutboundInput) error {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 120 || input.NodeID == "" || !protocol(input.Protocol, true) {
		return errors.New("invalid outbound name, node or protocol")
	}
	if input.Protocol != "direct" && input.Protocol != "block" && (input.TargetInboundID == "" && (input.Address == "" || input.Port < 1 || input.Port > 65535)) {
		return errors.New("outbound endpoint required")
	}
	if input.TargetNodeID == input.NodeID && input.TargetNodeID != "" {
		return errors.New("outbound cannot target its own node")
	}
	if !desiredstate.ValidFailover(input.FailurePolicy) || input.FailurePolicy == "BackupChain" {
		return errors.New("invalid outbound failure policy")
	}
	if input.Protocol == "http" && input.SupportUDP {
		return errors.New("HTTP outbound does not support UDP")
	}
	if input.BackupOutboundID == "" && desiredstate.BackupKind(input.FailurePolicy) != "" {
		return errors.New("backup outbound required")
	}
	return desiredstate.ValidateSettings(input.Protocol, input.Settings, false)
}
