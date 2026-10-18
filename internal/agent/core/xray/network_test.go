package xray

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// wgTestSecret is the fixed WireGuard private key the source-address scenarios
// render, so the derived public key and the golden files are stable.
const wgTestSecret = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="

// wgTestPublicKey derives the peer public key of wgTestSecret.
func wgTestPublicKey() string {
	data, err := base64.StdEncoding.DecodeString(wgTestSecret)
	if err != nil {
		panic(err)
	}
	key, err := ecdh.X25519().NewPrivateKey(data)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}

// sourceScenarioBase is the relay-shaped node every source-address scenario
// pins: a direct and a block outbound, a protocol relay outbound, and a group
// whose members are the dialing outbounds of the balancer path.
func sourceScenarioBase() desiredstate.State {
	return desiredstate.State{
		SchemaVersion: 1, Revision: 1, NodeID: "golden-node", Logging: desiredstate.Logging{DomainOnly: true},
		Users: []desiredstate.User{{ID: "user", Enabled: true}},
		Credentials: []desiredstate.Credential{
			{ID: "device-credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "golden-user", Secret: "golden-socks-secret"},
			{ID: "relay-credential", Protocol: "trojan", Internal: true, Secret: "independent-relay-password"},
		},
		Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, CredentialIDs: []string{"device-credential"}, DefaultOutbound: "group:egress-pool"}},
		Outbounds: []desiredstate.Outbound{
			{ID: "direct", Protocol: "direct", Enabled: true},
			{ID: "block", Protocol: "block", Enabled: true},
			{ID: "egress-primary", Protocol: "http", Address: "198.51.100.20", Port: 8080, Enabled: true},
			{ID: "egress-backup", Protocol: "trojan", Address: "198.51.100.21", Port: 443, Enabled: true, CredentialID: "relay-credential", Settings: desiredstate.ProtocolSettings{Security: "tls", ServerName: "exit.example.test"}},
		},
		OutboundGroups: []desiredstate.OutboundGroup{{ID: "egress-pool", Strategy: "primary", ProbeURL: "https://probe.example.test/generate_204", ProbeInterval: 30, Failover: "BackupOutbound", BackupTargetID: "egress-backup", Members: []desiredstate.GroupMember{{OutboundID: "egress-primary", Priority: 1, Weight: 1}, {OutboundID: "egress-backup", Priority: 2, Weight: 1}}}},
	}
}

func sourceScenarios() []goldenScenario {
	ipv4 := sourceScenarioBase()
	ipv4.Network = &desiredstate.Network{OutboundSourceIP: "192.0.2.10"}
	ipv6 := sourceScenarioBase()
	ipv6.Network = &desiredstate.Network{OutboundSourceIP: "2001:db8::10"}
	iface := sourceScenarioBase()
	iface.Network = &desiredstate.Network{OutboundInterface: "eth0"}
	both := sourceScenarioBase()
	both.Network = &desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth0"}
	wireguard := sourceScenarioBase()
	wireguard.Credentials = append(wireguard.Credentials, desiredstate.Credential{ID: "wg-credential", Protocol: "wireguard", Internal: true, Secret: wgTestSecret})
	wireguard.Outbounds = append(wireguard.Outbounds, desiredstate.Outbound{ID: "wg-out", Protocol: "wireguard", Enabled: true, Address: "198.51.100.30", Port: 51820, TargetNodeID: "exit-node", CredentialID: "wg-credential", Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: wgTestSecret, Addresses: []string{"10.66.0.2/32"}, MTU: 1420, Peers: []desiredstate.WireGuardPeer{{PublicKey: wgTestPublicKey(), CredentialID: "wg-credential", Endpoint: "198.51.100.30:51820", AllowedIPs: []string{"0.0.0.0/0"}, KeepAlive: 15}}}}})
	wireguard.Inbounds[0].DefaultOutbound = "wg-out"
	wireguard.Network = &desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth0"}
	return []goldenScenario{
		{name: "source-ipv4", state: ipv4},
		{name: "source-ipv6", state: ipv6},
		{name: "source-interface", state: iface},
		{name: "source-ipv4-interface", state: both},
		{name: "source-wireguard", state: wireguard},
	}
}

func TestGoldenOutboundSourceRenderedConfigurations(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	for _, scenario := range sourceScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			rendered, err := renderWithOptions(context.Background(), scenario.state, goldenAt, goldenOptions)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", scenario.name+".json")
			golden, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file: %v", err)
			}
			if string(golden) != string(rendered.Data) {
				t.Fatalf("%s does not match the rendered configuration:\n--- golden ---\n%s\n--- rendered ---\n%s", path, golden, rendered.Data)
			}
			if strings.Contains(strings.ToLower(string(rendered.Data)), "proxycontrol") {
				t.Fatalf("%s contains a fork key", path)
			}
			if binary == "" {
				return
			}
			validator := &Adapter{Path: binary, Directory: t.TempDir()}
			if err := validator.Validate(context.Background(), core.RenderedConfig{Data: golden}); err != nil {
				t.Fatalf("official core rejected the golden: %v", err)
			}
		})
	}
}

// TestOutboundSourceCoversEveryDialingOutbound asserts the shape of the pinning
// independently of the golden bytes: sendThrough and the interface socket option
// on every dialing outbound, the family strategy in the freedom settings, and
// the matching socket-option strategy on every other protocol. Blackhole never
// dials, so it keeps none of them.
func TestOutboundSourceCoversEveryDialingOutbound(t *testing.T) {
	for _, scenario := range sourceScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			rendered, err := renderWithOptions(context.Background(), scenario.state, goldenAt, goldenOptions)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Outbounds []struct {
					Tag         string `json:"tag"`
					Protocol    string `json:"protocol"`
					SendThrough string `json:"sendThrough"`
					Settings    struct {
						DomainStrategy string `json:"domainStrategy"`
					} `json:"settings"`
					Stream struct {
						Sockopt struct {
							Interface      string `json:"interface"`
							DomainStrategy string `json:"domainStrategy"`
						} `json:"sockopt"`
					} `json:"streamSettings"`
				} `json:"outbounds"`
			}
			if err := json.Unmarshal(rendered.Data, &document); err != nil {
				t.Fatal(err)
			}
			network := scenario.state.Network
			wantStrategy := "ForceIPv4"
			if network.OutboundSourceIP == "2001:db8::10" {
				wantStrategy = "ForceIPv6"
			}
			dialing := 0
			for _, outbound := range document.Outbounds {
				if outbound.Protocol == "blackhole" {
					if outbound.SendThrough != "" || outbound.Stream.Sockopt.Interface != "" {
						t.Fatalf("blackhole outbound %s was pinned", outbound.Tag)
					}
					continue
				}
				dialing++
				if outbound.SendThrough != network.OutboundSourceIP {
					t.Fatalf("outbound %s sendThrough = %q", outbound.Tag, outbound.SendThrough)
				}
				if outbound.Stream.Sockopt.Interface != network.OutboundInterface {
					t.Fatalf("outbound %s sockopt.interface = %q", outbound.Tag, outbound.Stream.Sockopt.Interface)
				}
				if network.OutboundSourceIP == "" {
					if outbound.Stream.Sockopt.DomainStrategy != "" || outbound.Settings.DomainStrategy != "" {
						t.Fatalf("interface-only scenario pinned a family on %s", outbound.Tag)
					}
					continue
				}
				if outbound.Protocol == "freedom" {
					if outbound.Settings.DomainStrategy != wantStrategy {
						t.Fatalf("freedom outbound %s settings.domainStrategy = %q", outbound.Tag, outbound.Settings.DomainStrategy)
					}
					continue
				}
				if outbound.Stream.Sockopt.DomainStrategy != wantStrategy {
					t.Fatalf("outbound %s sockopt.domainStrategy = %q", outbound.Tag, outbound.Stream.Sockopt.DomainStrategy)
				}
			}
			if dialing < 3 {
				t.Fatalf("only %d dialing outbounds rendered", dialing)
			}
		})
	}
}
