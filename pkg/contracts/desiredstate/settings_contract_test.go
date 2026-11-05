package desiredstate

import (
	"errors"
	"strings"
	"testing"
)

// The server-private structures are typed: a document that carries an extra
// option (an `extra` map, an xmux tuning block or any unknown key) is refused
// instead of being stored as opaque passthrough for a renderer to interpret.
func TestServerSettingsRefuseUnknownFields(t *testing.T) {
	settings := func(value string) string {
		return `{"schema_version":1,"revision":1,"node_id":"node","users":[],"credentials":[],"inbounds":[{"id":"in","protocol":"vless","listen":"0.0.0.0","port":443,"enabled":false,"credential_ids":[],"default_outbound":"direct","subscription_visible":false,"settings":` + value + `}],"outbounds":[],"outbound_groups":[],"proxy_chains":[],"policies":[],"logging":{"domain_only":true}}`
	}
	documents := []string{
		settings(`{"security":"reality","reality":{"private_key":"a","public_key":"b","handshake_target":"site.example:443","server_names":["site.example"],"short_ids":["abcd"],"spider_x":"/"}}`),
		settings(`{"security":"reality","reality":{"private_key":"a","public_key":"b","handshake_target":"site.example:443","server_names":["site.example"],"short_ids":["abcd"],"extra":{"site":"x"}}}`),
		settings(`{"transport":"xhttp","xhttp":{"host":"cdn.example","path":"/","mode":"auto","xmux":{"max_concurrency":1}}}`),
		settings(`{"transport":"xhttp","xhttp":{"host":"cdn.example","path":"/","mode":"auto","extra":{}}}`),
		settings(`{"security":"reality","raw_settings":{"pbk":"x"}}`),
	}
	for _, document := range documents {
		if _, err := Decode([]byte(document)); err == nil {
			t.Fatalf("opaque settings accepted: %s", document)
		}
	}
}

// M1 stores and validates the settings, but no Agent renderer exists yet: the
// render entry must refuse them, not drop them.
func TestAgentRendererRefusesServerSettings(t *testing.T) {
	state := State{Inbounds: []Inbound{{ID: "vless-in", Protocol: "vless", Settings: ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}}}}
	if err := ValidateServerSettingsRenderer(state); !errors.Is(err, ErrServerSettingsRendererPending) {
		t.Fatalf("inbound settings reached the renderer: %v", err)
	}
	xhttp := State{Inbounds: []Inbound{{ID: "xhttp-in", Protocol: "vless", Settings: ProtocolSettings{Transport: TransportXHTTP, XHTTP: testXHTTP()}}}}
	if err := ValidateServerSettingsRenderer(xhttp); !errors.Is(err, ErrServerSettingsRendererPending) {
		t.Fatalf("xhttp settings reached the renderer: %v", err)
	}
	outbound := State{Outbounds: []Outbound{{ID: "out", Protocol: "vless", Settings: ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}}}}
	if err := ValidateServerSettingsRenderer(outbound); !errors.Is(err, ErrServerSettingsRendererPending) {
		t.Fatalf("outbound settings reached the renderer: %v", err)
	}
	legacy := State{Inbounds: []Inbound{{ID: "vless-in", Protocol: "vless", Settings: ProtocolSettings{Security: "tls"}}}}
	if err := ValidateServerSettingsRenderer(legacy); err != nil {
		t.Fatalf("legacy state refused: %v", err)
	}
}

// The capability gate is the only thing that may authorize the new settings, and
// it reads one explicit per-core report. A missing report, an unknown version,
// another core's report and an unlisted combination all stay closed; the exact
// report — the synthetic positive case — opens exactly that combination.
func TestServerSettingsCapabilityGate(t *testing.T) {
	reality := ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}
	vision := ProtocolSettings{Security: SecurityReality, Reality: testReality(t), Flow: "xtls-rprx-vision"}
	legacy := ProtocolSettings{Security: "tls", Transport: "ws", Path: "/ws"}
	if _, required := ServerSettingsCombination("vless", legacy); required {
		t.Fatal("legacy settings must not need a settings contract")
	}
	combination, required := ServerSettingsCombination("vless", reality)
	if !required || combination.String() != "vless/inbound/reality/tcp/none" {
		t.Fatalf("combination: %v %v", combination, required)
	}
	if flow, _ := ServerSettingsCombination("vless", vision); flow.String() != "vless/inbound/reality/tcp/xtls-rprx-vision" {
		t.Fatalf("flow combination: %v", flow)
	}
	// A node that reports the core but no contract, and a report whose version or
	// core does not line up.
	if err := RequireSettingsCapability("xray", CoreSettingsCapability{Core: "xray"}, combination); err == nil || !strings.Contains(err.Error(), "has not reported") {
		t.Fatalf("missing report: %v", err)
	}
	unknown := CoreSettingsCapability{Core: "xray", Version: SettingsContractVersion + 1, Combinations: []SettingsCombination{combination}}
	if err := RequireSettingsCapability("xray", unknown, combination); err == nil || !strings.Contains(err.Error(), "unknown settings contract version") {
		t.Fatalf("unknown version: %v", err)
	}
	foreign := CoreSettingsCapability{Core: "sing-box", Version: SettingsContractVersion, Combinations: []SettingsCombination{combination}}
	if err := RequireSettingsCapability("xray", foreign, combination); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("another core's report: %v", err)
	}
	partial := CoreSettingsCapability{Core: "xray", Version: SettingsContractVersion, Combinations: []SettingsCombination{{Protocol: "vless", Direction: DirectionInbound, Security: "tls", Transport: TransportXHTTP}}}
	if err := RequireSettingsCapability("xray", partial, combination); err == nil || !strings.Contains(err.Error(), "does not report") {
		t.Fatalf("unlisted combination: %v", err)
	}
	full := CoreSettingsCapability{Core: "xray", Version: SettingsContractVersion, Combinations: []SettingsCombination{combination}}
	if err := RequireInboundServerSettings("xray", full, "vless", reality); err != nil {
		t.Fatalf("exact report refused: %v", err)
	}
	if err := RequireInboundServerSettings("xray", full, "vless", vision); err == nil {
		t.Fatal("a TCP combination authorized the Vision combination")
	}
	if err := RequireInboundServerSettings("xray", full, "vless", legacy); err != nil {
		t.Fatalf("legacy settings need no report: %v", err)
	}
	if SettingsContractVersion != 1 {
		t.Fatal("the understood settings contract version changed without a proto/panel update")
	}
}
