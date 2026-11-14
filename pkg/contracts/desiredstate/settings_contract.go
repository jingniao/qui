package desiredstate

import (
	"errors"
	"fmt"
)

// Self-hosted settings contract (compatibility plan §13.3 item 3, phase M1).
//
// A core may report the protocol flags it carries, but that says nothing about
// whether it can *apply* the newer server settings (xhttp, Reality) — a VLESS
// inbound flag is not evidence, and neither is a version string. The contract is
// therefore a separate per-core report: a version plus the exact (protocol,
// direction, security, transport, flow) combinations the core implements, carried
// by agentpb.CoreCapability.server_settings_contract_{version,combinations}.
//
// Zero, missing and unknown reports never authorize. No core advertises a
// combination before its renderer exists (Xray in M2, sing-box in M4), so a
// production Agent reports zero and every gate below stays closed.
const (
	// SettingsContractVersion is the settings-contract version this panel
	// understands. A report of another version is unknown, not "newer is fine":
	// the panel would not know which combinations it implies.
	SettingsContractVersion uint32 = 1
	// DirectionInbound and DirectionOutbound name the two halves a combination
	// applies to. A core that only opens an outbound cannot be authorized for an
	// inbound setting set.
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"
)

// SettingsCombination identifies one security/transport/flow combination of one
// protocol in one direction, e.g. vless/inbound/reality/tcp/vision. The empty flow
// and the "tcp" transport stay explicit so two spellings of the same combination
// cannot be authorized separately.
type SettingsCombination struct {
	Protocol  string `json:"protocol"`
	Direction string `json:"direction"`
	Security  string `json:"security"`
	Transport string `json:"transport"`
	Flow      string `json:"flow"`
}

// String is the stable, human-readable form used in diagnostics and tests.
func (c SettingsCombination) String() string {
	flow := c.Flow
	if flow == "" {
		flow = "none"
	}
	return c.Protocol + "/" + c.Direction + "/" + c.Security + "/" + c.Transport + "/" + flow
}

// Valid reports whether the combination names a protocol, direction and transport
// this contract can describe. It is a shape check only: whether the combination
// is legal for the protocol is ValidateSettings' job.
func (c SettingsCombination) Valid() bool {
	if c.Protocol == "" || c.Direction != DirectionInbound && c.Direction != DirectionOutbound {
		return false
	}
	if c.Security != "" && c.Security != "none" && c.Security != "tls" && c.Security != SecurityReality {
		return false
	}
	switch c.Transport {
	case "tcp", "ws", "grpc", "quic", TransportXHTTP:
	default:
		return false
	}
	if c.Flow != "" && c.Flow != "xtls-rprx-vision" {
		return false
	}
	return true
}

// CoreSettingsCapability is one core's explicit settings-contract report, keyed by
// the same core name the Agent uses in CoreCapability.name. An empty combinations
// list means "none": there is no implied default.
//
// CertificateFiles is the per-core report of the second server-private setting set
// (v1.4.7 section 3): whether this core's renderer can name certificate files on
// the node. It is not a versioned combination, because it changes no protocol,
// direction, security or transport — the same TLS listener is rendered either from
// pasted PEM or from two paths.
type CoreSettingsCapability struct {
	OutboundSourceSupported bool                  `json:"outbound_source_supported,omitempty"`
	Core                    string                `json:"core"`
	Version                 uint32                `json:"settings_contract_version"`
	Combinations            []SettingsCombination `json:"combinations,omitempty"`
	CertificateFiles        bool                  `json:"certificate_files,omitempty"`
}

// Supports reports whether this exact core declared this exact combination at the
// contract version this panel understands.
func (c CoreSettingsCapability) Supports(combination SettingsCombination) bool {
	if c.Core == "" || c.Version != SettingsContractVersion {
		return false
	}
	for _, supported := range c.Combinations {
		if supported == combination {
			return true
		}
	}
	return false
}

// RequireSettingsCapability refuses a combination unless the reported capability
// authorizes it. Every refusal is deliberate: a missing report, another version,
// another core's report or an unlisted combination are all unsupported.
func RequireSettingsCapability(core string, capability CoreSettingsCapability, combination SettingsCombination) error {
	if !combination.Valid() {
		return fmt.Errorf("invalid settings combination %s", combination.String())
	}
	if core == "" || capability.Core != core {
		return fmt.Errorf("settings contract report does not belong to the %q core", core)
	}
	if capability.Version == 0 {
		return fmt.Errorf("%s core has not reported the xhttp/Reality settings contract", core)
	}
	if capability.Version != SettingsContractVersion {
		return fmt.Errorf("%s core reports unknown settings contract version %d", core, capability.Version)
	}
	if !capability.Supports(combination) {
		return fmt.Errorf("%s core does not report the %s settings combination", core, combination.String())
	}
	return nil
}

// ServerSettingsCombination derives the combination one inbound settings block
// requires. The second result is false for every legacy combination (none/TLS
// over tcp, ws or gRPC), which therefore needs no settings-contract report.
func ServerSettingsCombination(protocol string, settings ProtocolSettings) (SettingsCombination, bool) {
	if !settings.HasServerSettings() {
		return SettingsCombination{}, false
	}
	combination := SettingsCombination{Protocol: protocol, Direction: DirectionInbound, Security: settings.Security, Transport: settings.Transport, Flow: settings.Flow}
	if combination.Security == "" {
		combination.Security = "none"
	}
	if combination.Transport == "" {
		combination.Transport = "tcp"
	}
	return combination, true
}

// RequireInboundServerSettings is the one gate every layer calls for a listener's
// settings: no report is needed for a legacy combination, and otherwise the
// resolved core must have declared exactly this combination. Node-side
// certificate files need their own per-core report, because an older Agent
// rejects the unknown settings fields instead of rendering them.
func RequireInboundServerSettings(core string, capability CoreSettingsCapability, protocol string, settings ProtocolSettings) error {
	if err := ValidateCoreServerSettings(core, settings); err != nil {
		return err
	}
	if settings.HasCertificateFiles() {
		if err := requireCertificateFiles(core, capability); err != nil {
			return errors.New("node certificate files are not available on this node yet: " + err.Error())
		}
	}
	combination, required := ServerSettingsCombination(protocol, settings)
	if !required {
		return nil
	}
	if err := RequireSettingsCapability(core, capability, combination); err != nil {
		return errors.New("xhttp/Reality settings are not available on this node yet: " + err.Error())
	}
	return nil
}

// requireCertificateFiles refuses node-side certificate files unless the resolved
// core reported the renderer that carries them. Another core's report and a
// missing one are both unsupported.
func requireCertificateFiles(core string, capability CoreSettingsCapability) error {
	switch {
	case core == "" || capability.Core != core:
		return fmt.Errorf("certificate file report does not belong to the %q core", core)
	case !capability.CertificateFiles:
		return fmt.Errorf("%s core has not reported certificate file support", core)
	}
	return nil
}

// ValidateCoreServerSettings preserves field semantics where a core has a
// narrower server model. Capabilities authorize combinations, not lossy mapping.
func ValidateCoreServerSettings(core string, settings ProtocolSettings) error {
	if core != CoreSingBox || !settings.HasServerSettings() {
		return nil
	}
	if settings.XHTTP != nil {
		return errors.New("sing-box does not support xhttp")
	}
	if reality := settings.Reality; reality != nil {
		if settings.Transport != "" && settings.Transport != "tcp" {
			return errors.New("sing-box Reality currently requires TCP")
		}
		if len(reality.ServerNames) != 1 {
			return errors.New("sing-box Reality requires exactly one server name")
		}
		if settings.ServerName != "" && settings.ServerName != reality.ServerNames[0] {
			return errors.New("sing-box Reality server name conflicts with its permitted server name")
		}
	}
	return nil
}
