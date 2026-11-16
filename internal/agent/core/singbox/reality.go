package singbox

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"os"
	"time"

	"proxycontrol/internal/agent/core"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Capability reporting requires both this Agent's typed renderer and the actual
// binary accepting its output. A version banner alone never opens the contract.
// Native check does not connect to the loopback handshake target or bind ports.
func probeRealitySettings(ctx context.Context, executable string) []*pb.SettingsCombination {
	directory, err := os.MkdirTemp("", "qui-singbox-reality-probe-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(directory)
	adapter := &Adapter{Path: executable, Directory: directory}
	return append(probeRealityCombinations(ctx, adapter.Validate), probeOutboundCombinations(ctx, adapter.Validate)...)
}

func probeRealityCombinations(ctx context.Context, validate func(context.Context, core.RenderedConfig) error) []*pb.SettingsCombination {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "reality-probe", Logging: desiredstate.Logging{DomainOnly: true},
		Users:       []desiredstate.User{{ID: "probe-user", Enabled: true}},
		Credentials: []desiredstate.Credential{{ID: "probe-credential", UserID: "probe-user", DeviceID: "probe-device", Protocol: "vless", Secret: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}},
		Inbounds: []desiredstate.Inbound{{ID: "probe-in", Core: "sing-box", Protocol: "vless", Listen: "127.0.0.1", Port: 24443, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"probe-credential"},
			Settings: desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: &desiredstate.RealityServerSettings{
				PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
				HandshakeTarget: "127.0.0.1:24444", ServerNames: []string{"probe.invalid"}, ShortIDs: []string{"abcd", "0123456789abcdef"},
			}}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	var combinations []*pb.SettingsCombination
	for _, flow := range []string{"", "xtls-rprx-vision"} {
		state.Inbounds[0].Settings.Flow = flow
		rendered, err := Render(ctx, state, time.Now())
		if err != nil || validate(ctx, rendered) != nil {
			continue
		}
		combinations = append(combinations, &pb.SettingsCombination{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: desiredstate.SecurityReality, Transport: "tcp", Flow: flow})
	}
	return combinations
}

// probeOutboundCombinations reports the outbound client combinations (v1.6.0)
// this renderer emits and the installed binary accepts. sing-box has no xhttp and
// dials Reality over TCP only, and uTLS needs a build with it.
func probeOutboundCombinations(ctx context.Context, validate func(context.Context, core.RenderedConfig) error) []*pb.SettingsCombination {
	var combinations []*pb.SettingsCombination
	for _, c := range desiredstate.OutboundClientCombinations() {
		settings, err := core.ProbeClientSettings(c)
		if err != nil || desiredstate.ValidateCoreClientSettings(desiredstate.CoreSingBox, settings) != nil {
			continue
		}
		state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "outbound-probe", Logging: desiredstate.Logging{DomainOnly: true},
			Users:       []desiredstate.User{{ID: "probe-user", Enabled: true}},
			Credentials: []desiredstate.Credential{{ID: "probe-local", UserID: "probe-user", DeviceID: "probe-device", Protocol: "socks5", Username: "probe", Secret: "probe-secret"}, {ID: "probe-upstream", Protocol: c.Protocol, Secret: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}},
			Inbounds:    []desiredstate.Inbound{{ID: "probe-in", Core: "sing-box", Protocol: "socks5", Listen: "127.0.0.1", Port: 24445, Enabled: true, DefaultOutbound: "probe-out", CredentialIDs: []string{"probe-local"}}},
			Outbounds:   []desiredstate.Outbound{{ID: "probe-out", Protocol: c.Protocol, Address: "127.0.0.1", Port: 24446, Enabled: true, CredentialID: "probe-upstream", Settings: settings, SupportTCP: true}, {ID: "direct", Protocol: "direct", Enabled: true}}}
		rendered, err := Render(ctx, state, time.Now())
		if err != nil || validate(ctx, rendered) != nil {
			continue
		}
		combinations = append(combinations, &pb.SettingsCombination{Protocol: c.Protocol, Direction: c.Direction, Security: c.Security, Transport: c.Transport, Flow: c.Flow})
	}
	return combinations
}
