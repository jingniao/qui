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
	return probeRealityCombinations(ctx, adapter.Validate)
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
