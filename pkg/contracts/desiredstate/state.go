// Package desiredstate defines the shared, validated business configuration.
package desiredstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

type State struct {
	SchemaVersion  int             `json:"schema_version"`
	Revision       int64           `json:"revision"`
	NodeID         string          `json:"node_id"`
	Users          []User          `json:"users"`
	Credentials    []Credential    `json:"credentials"`
	Inbounds       []Inbound       `json:"inbounds"`
	Outbounds      []Outbound      `json:"outbounds"`
	OutboundGroups []OutboundGroup `json:"outbound_groups"`
	ProxyChains    []ProxyChain    `json:"proxy_chains"`
	Policies       []Policy        `json:"policies"`
	Logging        Logging         `json:"logging"`
	Network        *Network        `json:"network,omitempty"`
}
type User struct {
	ID                string `json:"id"`
	Enabled           bool   `json:"enabled"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	ConnectionLimited bool   `json:"connection_limited,omitempty"`
}

// UnmarshalJSON tolerates the pre-4b.3c "budget" and "connection_allowance"
// user keys so states written by older panels still decode. Every other
// unknown user field remains rejected.
func (u *User) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	delete(fields, "budget")
	delete(fields, "connection_allowance")
	filtered, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	type plain User
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(filtered))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*u = User(decoded)
	return nil
}

type Credential struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	DeviceID  string `json:"device_id"`
	Protocol  string `json:"protocol"`
	Username  string `json:"username,omitempty"`
	Secret    string `json:"secret"`
	Internal  bool   `json:"internal"`
	PublicKey string `json:"public_key,omitempty"`
}
type Inbound struct {
	Core                string           `json:"core,omitempty"`
	ID                  string           `json:"id"`
	Protocol            string           `json:"protocol"`
	Listen              string           `json:"listen"`
	Port                int              `json:"port"`
	Enabled             bool             `json:"enabled"`
	CredentialIDs       []string         `json:"credential_ids"`
	DefaultOutbound     string           `json:"default_outbound"`
	SubscriptionVisible bool             `json:"subscription_visible"`
	Settings            ProtocolSettings `json:"settings"`
}
type Outbound struct {
	ID             string           `json:"id"`
	Protocol       string           `json:"protocol"`
	Address        string           `json:"address,omitempty"`
	Port           int              `json:"port,omitempty"`
	Enabled        bool             `json:"enabled"`
	CredentialID   string           `json:"credential_id,omitempty"`
	TargetNodeID   string           `json:"target_node_id,omitempty"`
	Settings       ProtocolSettings `json:"settings"`
	FailurePolicy  string           `json:"failure_policy,omitempty"`
	BackupTargetID string           `json:"backup_target_id,omitempty"`
	SupportTCP     bool             `json:"support_tcp,omitempty"`
	SupportUDP     bool             `json:"support_udp,omitempty"`
	ProbeURL       string           `json:"probe_url,omitempty"`
	ProbeInterval  int              `json:"probe_interval,omitempty"`
}
type ProtocolSettings struct {
	Forward        *ForwardSettings   `json:"forward,omitempty"`
	Transport      string             `json:"transport,omitempty"`
	Security       string             `json:"security,omitempty"`
	ServerName     string             `json:"server_name,omitempty"`
	Method         string             `json:"method,omitempty"`
	Flow           string             `json:"flow,omitempty"`
	CertificatePEM string             `json:"certificate_pem,omitempty"`
	PrivateKeyPEM  string             `json:"private_key_pem,omitempty"`
	TrustedCAPEM   string             `json:"trusted_ca_pem,omitempty"`
	Path           string             `json:"path,omitempty"`
	ServiceName    string             `json:"service_name,omitempty"`
	WireGuard      *WireGuardSettings `json:"wireguard,omitempty"`
	QUIC           *QUICSettings      `json:"quic,omitempty"`
}

type ForwardSettings struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Network string `json:"network"`
}

type QUICSettings struct {
	UpMbps            int    `json:"up_mbps,omitempty"`
	DownMbps          int    `json:"down_mbps,omitempty"`
	CongestionControl string `json:"congestion_control,omitempty"`
	UDPRelayMode      string `json:"udp_relay_mode,omitempty"`
}
type WireGuardSettings struct {
	SecretKey string          `json:"secret_key"`
	Addresses []string        `json:"addresses"`
	Peers     []WireGuardPeer `json:"peers"`
	MTU       int             `json:"mtu,omitempty"`
}
type WireGuardPeer struct {
	PublicKey    string   `json:"public_key"`
	Endpoint     string   `json:"endpoint,omitempty"`
	AllowedIPs   []string `json:"allowed_ips"`
	KeepAlive    int      `json:"keep_alive,omitempty"`
	CredentialID string   `json:"credential_id,omitempty"`
}
type OutboundGroup struct {
	ID             string        `json:"id"`
	Strategy       string        `json:"strategy"`
	Members        []GroupMember `json:"members"`
	ProbeURL       string        `json:"probe_url,omitempty"`
	ProbeInterval  int           `json:"probe_interval,omitempty"`
	Failover       string        `json:"failover,omitempty"`
	BackupTargetID string        `json:"backup_target_id,omitempty"`
}
type GroupMember struct {
	OutboundID string `json:"outbound_id"`
	Priority   int    `json:"priority"`
	Weight     int    `json:"weight"`
}
type ProxyChain struct {
	ID              string   `json:"id"`
	NodeIDs         []string `json:"node_ids"`
	OutboundIDs     []string `json:"outbound_ids"`
	Failover        string   `json:"failover"`
	FinalOutboundID string   `json:"final_outbound_id,omitempty"`
	BackupTargetID  string   `json:"backup_target_id,omitempty"`
}
type Policy struct {
	ID       string         `json:"id"`
	Priority int            `json:"priority"`
	Enabled  bool           `json:"enabled"`
	Scope    Scope          `json:"scope"`
	Match    Match          `json:"match"`
	Action   Action         `json:"action"`
	Name     string         `json:"name,omitempty"`
	Timezone string         `json:"timezone,omitempty"`
	Logging  *PolicyLogging `json:"logging,omitempty"`
}
type PolicyLogging struct {
	LogDomain    bool `json:"log_domain"`
	LogTraffic   bool `json:"log_traffic"`
	AlertOnBlock bool `json:"alert_on_block"`
}
type Scope struct {
	UserIDs   []string `json:"user_ids,omitempty"`
	DeviceIDs []string `json:"device_ids,omitempty"`
	NodeIDs   []string `json:"node_ids,omitempty"`
	GroupIDs  []string `json:"group_ids,omitempty"`
}
type Match struct {
	Domains    []string `json:"domains,omitempty"`
	Suffixes   []string `json:"suffixes,omitempty"`
	CIDRs      []string `json:"cidrs,omitempty"`
	Ports      []string `json:"ports,omitempty"`
	Network    string   `json:"network,omitempty"`
	TimeRange  string   `json:"time_range,omitempty"`
	ServiceIDs []string `json:"service_ids,omitempty"`
}
type Action struct {
	Type     string `json:"type"`
	TargetID string `json:"target_id,omitempty"`
	Failover string `json:"failover,omitempty"`
	BackupID string `json:"backup_id,omitempty"`
}
type Logging struct {
	DomainOnly bool `json:"domain_only"`
}

func Decode(data []byte) (State, error) {
	var state State
	if len(data) > 2<<20 {
		return state, errors.New("DesiredState exceeds 2 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, errors.New("invalid DesiredState JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return state, errors.New("DesiredState must contain one JSON object")
	}
	// Compatibility: revisions published before the weighted strategy was
	// removed (plan D4) are read as round_robin, so a node that still holds one
	// never fails to load or apply it.
	for index := range state.OutboundGroups {
		if state.OutboundGroups[index].Strategy == "weighted" {
			state.OutboundGroups[index].Strategy = "round_robin"
		}
	}
	// An empty network block is normalized away so the decoded state marshals
	// back to the bytes of a state that never carried the field.
	if state.Network != nil && state.Network.Empty() {
		state.Network = nil
	}
	return state, state.Validate()
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return id != "." && id != ".."
}

// The fork relay association envelope predicates are gone (plan D1/D4): official
// upstream cores carry relay traffic over ordinary protocol outbounds, so no
// state needs an envelope capability and no publication is refused for it.

func (s State) Validate() error {
	if s.SchemaVersion != 1 {
		return errors.New("unsupported DesiredState schema version")
	}
	if s.Revision < 1 || !validID(s.NodeID) {
		return errors.New("revision and node ID are required")
	}
	if !s.Logging.DomainOnly {
		return errors.New("domain-only logging is required")
	}
	if len(s.Inbounds) > 32 || len(s.Outbounds) > 64 || len(s.Credentials) > 256 || len(s.Policies) > 512 {
		return errors.New("DesiredState resource limit exceeded")
	}
	if s.Network != nil {
		if err := s.Network.Validate(); err != nil {
			return err
		}
	}
	ids := map[string]bool{}
	add := func(id string) error {
		if !validID(id) || ids[id] {
			return errors.New("invalid or duplicate resource ID")
		}
		ids[id] = true
		return nil
	}
	users := map[string]bool{}
	for _, user := range s.Users {
		if err := add(user.ID); err != nil {
			return err
		}
		if user.ExpiresAt != "" {
			expires, err := time.Parse(time.RFC3339Nano, user.ExpiresAt)
			if err != nil || expires.Year() < 2000 || expires.Year() > 9999 {
				return errors.New("invalid user expiration time")
			}
		}
		users[user.ID] = user.Enabled
	}
	credentials := map[string]Credential{}
	for _, credential := range s.Credentials {
		if err := add(credential.ID); err != nil {
			return err
		}
		if credential.UserID != "" && !users[credential.UserID] {
			return errors.New("credential refers to inactive or missing user")
		}
		if (len(credential.Secret) < 1 && (credential.Protocol != "wireguard" || credential.PublicKey == "")) || len(credential.Secret) > 512 || len(credential.Username) > 128 {
			return errors.New("invalid credential")
		}
		if credential.Protocol == "wireguard" && credential.PublicKey != "" {
			if err := ValidateWireGuardPublicKey(credential.PublicKey); err != nil {
				return err
			}
		}
		if credential.Protocol == "tuic" {
			if err := ValidateUUID(credential.Username); err != nil {
				return err
			}
		}
		credentials[credential.ID] = credential
	}
	outbounds := map[string]bool{}
	for _, outbound := range s.Outbounds {
		if err := add(outbound.ID); err != nil {
			return err
		}
		if !SupportsOutbound(outbound.Protocol) {
			return errors.New("unsupported outbound protocol")
		}
		if outbound.CredentialID != "" {
			credential, exists := credentials[outbound.CredentialID]
			if !exists || credential.Protocol != outbound.Protocol {
				return errors.New("outbound credential missing")
			}
			if outbound.TargetNodeID != "" && (!credential.Internal || credential.UserID != "" || credential.DeviceID != "") {
				return errors.New("node relay requires separate internal credential")
			}
		}
		if err := ValidateSettings(outbound.Protocol, outbound.Settings, false); err != nil {
			return err
		}
		if outbound.Protocol != "direct" && outbound.Protocol != "block" && (outbound.Address == "" || outbound.Port < 1 || outbound.Port > 65535) {
			return errors.New("outbound endpoint required")
		}
		outbounds[outbound.ID] = outbound.Enabled
	}
	for _, group := range s.OutboundGroups {
		if err := add(group.ID); err != nil {
			return err
		}
		if err := validateGroup(group, outbounds); err != nil {
			return err
		}
		outbounds[group.ID] = true
	}
	for _, chain := range s.ProxyChains {
		if len(chain.NodeIDs) > 0 && chain.NodeIDs[0] == s.NodeID && len(chain.OutboundIDs) > 0 && outbounds[chain.OutboundIDs[0]] {
			outbounds[chain.ID] = true
		}
	}
	ports := map[string]bool{}
	for _, inbound := range s.Inbounds {
		if inbound.Core != "" && inbound.Core != "xray" && inbound.Core != "sing-box" {
			return errors.New("unsupported inbound core")
		}
		if inbound.Protocol == "forward" && (inbound.SubscriptionVisible || len(inbound.CredentialIDs) != 0) {
			return errors.New("forward inbounds cannot carry credentials or enter subscriptions")
		}
		if err := add(inbound.ID); err != nil {
			return err
		}
		if !SupportsInbound(inbound.Protocol) {
			return errors.New("unsupported inbound protocol")
		}
		ip := net.ParseIP(inbound.Listen)
		if ip == nil || inbound.Port < 1 || inbound.Port > 65535 {
			return errors.New("invalid inbound listener")
		}
		if inbound.Enabled {
			port := fmt.Sprint(inbound.Port)
			if ports[port] {
				return errors.New("inbound port conflict")
			}
			ports[port] = true
			if !targetExists(inbound.DefaultOutbound, outbounds) {
				return errors.New("every inbound needs an enabled default outbound")
			}
		}
		for _, id := range inbound.CredentialIDs {
			credential, exists := credentials[id]
			if !exists || credential.Protocol != inbound.Protocol {
				return errors.New("inbound credential mismatch")
			}
			if credential.Internal && inbound.SubscriptionVisible {
				return errors.New("internal relay inbound cannot enter subscriptions")
			}
		}
		if err := ValidateSettings(inbound.Protocol, inbound.Settings, true); err != nil {
			return err
		}
		if inbound.Enabled && inbound.Protocol != "forward" && len(inbound.CredentialIDs) == 0 && (inbound.Protocol != "socks5" && inbound.Protocol != "http" || !ip.IsLoopback()) {
			return errors.New("public proxy inbounds require independent credentials")
		}
	}
	for _, chain := range s.ProxyChains {
		if err := add(chain.ID); err != nil {
			return err
		}
		if len(chain.NodeIDs) < 2 || len(chain.NodeIDs) > 3 {
			return errors.New("proxy chains require two or three nodes")
		}
		seen := map[string]bool{}
		for _, id := range chain.NodeIDs {
			if !validID(id) || seen[id] {
				return errors.New("proxy chain loop or duplicate node")
			}
			seen[id] = true
		}
	}
	return ValidateScheduling(s)
}
