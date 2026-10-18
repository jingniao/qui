package desiredstate

// Protocol capability basis.
//
// This file is the single source of truth for which protocols the platform
// accepts, in which direction, and which cross-cutting capabilities each one
// carries. Server-side validation (ValidateSettings, State.Validate,
// proxy.ValidateInbound/ValidateOutbound, proxy.Service.SaveInbound/SaveOutbound)
// and the read-only API exposed to the web options both derive from this table,
// so the page options and the server checks can never drift apart.
//
// The table describes the official upstream cores only (plan D1): unmodified
// Xray 26.x and sing-box 1.14.x release binaries. Every row carries a per-core
// CoreSupport entry (Cores), so a node whose core is known can be checked
// against the same basis (CoreSupportsInbound/CoreSupportsOutbound). The
// fork-only capability dimensions (egress_proof, inbound_bytes) were removed in
// Phase 6.4/6.5: no official core reports them.
//
// docs/protocol-support-matrix.md mirrors this table and records what upstream
// cannot do (plan D4/D5): per-user traffic and online counts exist on Xray
// nodes only, sing-box nodes report node totals, and WireGuard has no per-pee
// user attribution on sing-box.
//
// It is deliberately a hand-curated matrix, not a cartesian product: each
// protocol lists the capability tokens it implements, the tokens it still owes,
// and the tokens that do not apply. Values are grounded in the current
// ValidateSettings branches, the node capability contract
// (pkg/contracts/agentpb ProtocolCapability) and the target/chain/scheduler/
// subscription checks. Nothing here claims runtime acceptance.

// Support status tokens. "implemented" means source exists and is pending this
// version's acceptance run; "pending" means not implemented; "not_applicable"
// means the combination is outside the protocol's definition.
const (
	SupportImplemented   = "implemented_pending_acceptance"
	SupportPending       = "pending"
	SupportNotApplicable = "not_applicable"
)

// Capability dimension tokens shared by the matrix and the API/UI.
const (
	DimUserIdentity  = "user_identity"
	DimAccountMeter  = "account_metering"
	DimQuota         = "quota"
	DimConcurrency   = "concurrency"
	DimSubscription  = "subscription"
	DimUDP           = "udp"
	DimIPv6          = "ipv6"
	DimTLS           = "tls"
	DimQUIC          = "quic"
	DimTransportWS   = "transport_ws"
	DimTransportGRPC = "transport_grpc"
)

// Core names of the per-core rows. They match the core name an Agent reports in
// agentpb.Capabilities.CoreName.
const (
	CoreXray    = "xray"
	CoreSingBox = "sing-box"
)

// CoreSupport is one official core's share of a protocol row: whether that core
// can terminate an inbound, open an outbound, and carry TCP and UDP. Every row
// carries one entry per core the matrix describes (CoreXray, CoreSingBox), so a
// zero value means "that core cannot carry this protocol"; a core name that is
// not a key of any row is not described by the matrix at all.
type CoreSupport struct {
	Inbound  bool `json:"inbound"`
	Outbound bool `json:"outbound"`
	TCP      bool `json:"tcp"`
	UDP      bool `json:"udp"`
}

// ProtocolSupport is one row of the frozen support matrix. Directions and
// transports are explicit; capability dimensions are grouped by status so the
// UI can hide an unsupported option and the server can refuse it from the same
// list.
type ProtocolSupport struct {
	Protocol      string                 `json:"protocol"`
	Direction     string                 `json:"direction"` // inbound, outbound or both
	Transport     []string               `json:"transport"` // tcp and/or udp
	AddressFamily []string               `json:"address_family"`
	Security      []string               `json:"security"` // none, tls, quic
	Cores         map[string]CoreSupport `json:"cores"`    // keyed by CoreXray / CoreSingBox
	Implemented   []string               `json:"implemented"`
	Pending       []string               `json:"pending"`
	NotApplicable []string               `json:"not_applicable"`
	Source        string                 `json:"source"`
}

// ProtocolSupportTable returns the frozen, ordered support matrix. The order is
// stable so the web option lists and the server checks present the same basis.
func ProtocolSupportTable() []ProtocolSupport {
	identity := []string{DimUserIdentity, DimAccountMeter, DimQuota, DimConcurrency, DimSubscription, DimIPv6}
	identityUDP := append([]string{}, append(identity, DimUDP)...)
	identityStream := append([]string{}, append(identityUDP, DimTLS, DimTransportWS, DimTransportGRPC)...)
	identityQUIC := append([]string{}, append(identityUDP, DimTLS, DimQUIC)...)
	both := map[string]CoreSupport{CoreXray: {Inbound: true, Outbound: true, TCP: true, UDP: true}, CoreSingBox: {Inbound: true, Outbound: true, TCP: true, UDP: true}}
	return []ProtocolSupport{
		{
			Protocol: "http", Direction: "both", Transport: []string{"tcp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none"},
			Cores:         map[string]CoreSupport{CoreXray: {Inbound: true, Outbound: true, TCP: true}, CoreSingBox: {Inbound: true, Outbound: true, TCP: true}},
			Implemented:   identity,
			Pending:       []string{},
			NotApplicable: []string{DimUDP, DimTLS, DimQUIC, DimTransportWS, DimTransportGRPC},
			Source:        "proxy/model.go rejects HTTP+UDP; both official cores report http udp=false",
		},
		{
			Protocol: "socks5", Direction: "both", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none"},
			Cores:         both,
			Implemented:   identityUDP,
			Pending:       []string{},
			NotApplicable: []string{DimTLS, DimQUIC, DimTransportWS, DimTransportGRPC},
			Source:        "ValidateSettings; both official cores report socks5 udp=true",
		},
		{
			Protocol: "shadowsocks", Direction: "both", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none"},
			Cores:         both,
			Implemented:   identityUDP,
			Pending:       []string{},
			NotApplicable: []string{DimTLS, DimQUIC, DimTransportWS, DimTransportGRPC},
			Source:        "ValidateSettings cipher allowlist; both official cores report shadowsocks udp=true",
		},
		{
			Protocol: "vless", Direction: "both", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none", "tls"},
			Cores:         both,
			Implemented:   identityStream,
			Pending:       []string{},
			NotApplicable: []string{DimQUIC},
			Source:        "ValidateSettings transport/flow/security; both official cores render VLESS with ws and grpc transports",
		},
		{
			Protocol: "trojan", Direction: "both", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"tls"},
			Cores:         both,
			Implemented:   identityStream,
			Pending:       []string{},
			NotApplicable: []string{DimQUIC},
			Source:        "ValidateSettings requires TLS for trojan; both official cores render ws and grpc transports",
		},

		{
			Protocol: "wireguard", Direction: "both", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none"},
			Cores:         map[string]CoreSupport{CoreXray: {Inbound: true, Outbound: true, TCP: true, UDP: true}, CoreSingBox: {Outbound: true, TCP: true, UDP: true}},
			Implemented:   identityUDP,
			Pending:       []string{},
			NotApplicable: []string{DimTLS, DimQUIC, DimTransportWS, DimTransportGRPC},
			Source:        "proxy/wireguard_pool.go per-family pool; sing-box reports the outbound only (no per-peer user attribution, plan D4); Xray needs major>=25 for the inbound",
		},
		{
			Protocol: "hysteria2", Direction: "both", Transport: []string{"udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"tls", "quic"},
			Cores:         map[string]CoreSupport{CoreXray: {}, CoreSingBox: {Inbound: true, Outbound: true, TCP: true, UDP: true}},
			Implemented:   identityQUIC,
			Pending:       []string{},
			NotApplicable: []string{DimTransportWS, DimTransportGRPC},
			Source:        "sing-box only, and only when built with_quic (official 1.14.3 has it); ValidateSettings native QUIC",
		},
		{
			Protocol: "tuic", Direction: "both", Transport: []string{"udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"tls", "quic"},
			Cores:         map[string]CoreSupport{CoreXray: {}, CoreSingBox: {Inbound: true, Outbound: true, TCP: true, UDP: true}},
			Implemented:   identityQUIC,
			Pending:       []string{},
			NotApplicable: []string{DimTransportWS, DimTransportGRPC},
			Source:        "sing-box only, and only when built with_quic (official 1.14.3 has it); ValidateSettings native QUIC + UUID identity",
		},
		{
			Protocol: "direct", Direction: "outbound", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none"},
			Cores:         map[string]CoreSupport{CoreXray: {Outbound: true, TCP: true, UDP: true}, CoreSingBox: {Outbound: true, TCP: true, UDP: true}},
			Implemented:   []string{DimAccountMeter, DimUDP, DimIPv6},
			Pending:       []string{},
			NotApplicable: []string{DimUserIdentity, DimQuota, DimConcurrency, DimSubscription, DimTLS, DimQUIC, DimTransportWS, DimTransportGRPC},
			Source:        "ValidateSettings rejects transports for direct/block; both official cores render the direct outbound",
		},
		{
			Protocol: "block", Direction: "outbound", Transport: []string{"tcp", "udp"}, AddressFamily: []string{"ipv4", "ipv6"}, Security: []string{"none"},
			Cores:         map[string]CoreSupport{CoreXray: {Outbound: true, TCP: true, UDP: true}, CoreSingBox: {Outbound: true, TCP: true, UDP: true}},
			Implemented:   []string{DimUDP, DimIPv6},
			Pending:       []string{},
			NotApplicable: []string{DimUserIdentity, DimAccountMeter, DimQuota, DimConcurrency, DimSubscription, DimTLS, DimQUIC, DimTransportWS, DimTransportGRPC},
			Source:        "ValidateSettings rejects transports for direct/block; both official cores render the block outbound",
		},
	}
}

func supportByProtocol() map[string]ProtocolSupport {
	table := ProtocolSupportTable()
	result := make(map[string]ProtocolSupport, len(table))
	for _, row := range table {
		result[row.Protocol] = row
	}
	return result
}

// SupportsInbound reports whether a protocol may terminate a user/relay inbound.
func SupportsInbound(protocol string) bool {
	row, ok := supportByProtocol()[protocol]
	return ok && (row.Direction == "inbound" || row.Direction == "both")
}

// SupportsOutbound reports whether a protocol may be used as an outbound,
// including the direct and block actions.
func SupportsOutbound(protocol string) bool {
	row, ok := supportByProtocol()[protocol]
	return ok && (row.Direction == "outbound" || row.Direction == "both")
}

// CoreSupportFor returns the per-core row of one protocol. The second result is
// false when that core cannot carry the protocol at all.
func CoreSupportFor(core, protocol string) (CoreSupport, bool) {
	row, ok := supportByProtocol()[protocol]
	if !ok {
		return CoreSupport{}, false
	}
	support, ok := row.Cores[core]
	return support, ok
}

// CoreSupportsInbound reports whether one official core can terminate an
// inbound of this protocol.
func CoreSupportsInbound(core, protocol string) bool {
	support, ok := CoreSupportFor(core, protocol)
	return ok && support.Inbound
}

// CoreSupportsOutbound reports whether one official core can open an outbound
// of this protocol.
func CoreSupportsOutbound(core, protocol string) bool {
	support, ok := CoreSupportFor(core, protocol)
	return ok && support.Outbound
}

// SupportedInboundProtocols returns the ordered inbound protocol basis.
func SupportedInboundProtocols() []string {
	result := []string{}
	for _, row := range ProtocolSupportTable() {
		if row.Direction == "inbound" || row.Direction == "both" {
			result = append(result, row.Protocol)
		}
	}
	return result
}

// SupportedOutboundProtocols returns the ordered outbound protocol basis.
func SupportedOutboundProtocols() []string {
	result := []string{}
	for _, row := range ProtocolSupportTable() {
		if row.Direction == "outbound" || row.Direction == "both" {
			result = append(result, row.Protocol)
		}
	}
	return result
}
