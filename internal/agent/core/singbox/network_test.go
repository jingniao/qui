package singbox

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
	"proxycontrol/pkg/contracts/scheduler"
)

// wgTestSecret is the fixed WireGuard private key the source-address scenarios
// render, so the derived public key and the golden files are stable.
const wgTestSecret = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="

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

// sourceScenario is one source-address golden: the state, the render options it
// needs (a WireGuard endpoint is only rendered when the node reports the
// capability), and the tag of the outbound WireGuard endpoint to check.
type sourceScenario struct {
	name     string
	state    desiredstate.State
	options  renderOptions
	endpoint string
}

// sourceScenarioBase is the relay-shaped node every source-address scenario
// pins: a direct and a block outbound, a protocol relay outbound, and a
// round-robin group, which renders the urltest outbound that must not be
// pinned.
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
		OutboundGroups: []desiredstate.OutboundGroup{{ID: "egress-pool", Strategy: "round_robin", ProbeURL: "https://probe.example.test/generate_204", ProbeInterval: 30, Failover: "Block", Members: []desiredstate.GroupMember{{OutboundID: "egress-primary", Weight: 1}, {OutboundID: "egress-backup", Weight: 1}}}},
	}
}

func sourceScenarios() []sourceScenario {
	// Fresh health for both group members, so the round-robin group renders its
	// urltest outbound instead of falling back to Block.
	options := goldenOptions
	options.health = map[string]scheduler.Health{
		"egress-primary": {Healthy: true, CheckedAt: goldenAt},
		"egress-backup":  {Healthy: true, CheckedAt: goldenAt},
	}
	build := func(network *desiredstate.Network) sourceScenario {
		state := sourceScenarioBase()
		state.Network = network
		return sourceScenario{name: "", state: state, options: options}
	}
	ipv4 := build(&desiredstate.Network{OutboundSourceIP: "192.0.2.10"})
	ipv4.name = "source-ipv4"
	ipv6 := build(&desiredstate.Network{OutboundSourceIP: "2001:db8::10"})
	ipv6.name = "source-ipv6"
	iface := build(&desiredstate.Network{OutboundInterface: "eth0"})
	iface.name = "source-interface"
	both := build(&desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth0"})
	both.name = "source-ipv4-interface"
	wireguard := sourceScenarioBase()
	wireguard.Credentials = append(wireguard.Credentials,
		desiredstate.Credential{ID: "wg-credential", Protocol: "wireguard", Internal: true, Secret: wgTestSecret},
		desiredstate.Credential{ID: "wg-device", UserID: "user", DeviceID: "device", Protocol: "wireguard", Secret: wgTestSecret})
	wireguard.Outbounds = append(wireguard.Outbounds, desiredstate.Outbound{ID: "wg-out", Protocol: "wireguard", Enabled: true, Address: "198.51.100.30", Port: 51820, TargetNodeID: "exit-node", CredentialID: "wg-credential", Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: wgTestSecret, Addresses: []string{"10.66.0.2/32"}, MTU: 1420, Peers: []desiredstate.WireGuardPeer{{PublicKey: wgTestPublicKey(), Endpoint: "198.51.100.30:51820", AllowedIPs: []string{"0.0.0.0/0"}, KeepAlive: 15}}}}})
	// The inbound WireGuard endpoint only listens: it must stay unpinned.
	wireguard.Inbounds = append(wireguard.Inbounds, desiredstate.Inbound{ID: "wg-entry", Protocol: "wireguard", Listen: "127.0.0.1", Port: 51821, Enabled: true, CredentialIDs: []string{"wg-device"}, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: wgTestSecret, Addresses: []string{"10.66.0.1/32"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: wgTestPublicKey(), CredentialID: "wg-device", AllowedIPs: []string{"10.66.0.2/32"}}}}}})
	wireguard.Network = &desiredstate.Network{OutboundSourceIP: "192.0.2.10", OutboundInterface: "eth0"}
	wgOptions := options
	wgOptions.wireGuardOutbound, wgOptions.wireGuardInbound = true, true
	return []sourceScenario{ipv4, ipv6, iface, both, {name: "source-wireguard", state: wireguard, options: wgOptions, endpoint: "wg-out"}}
}

type dialFields struct {
	Type             string `json:"type"`
	Tag              string `json:"tag"`
	ListenPort       int    `json:"listen_port"`
	BindInterface    string `json:"bind_interface"`
	Inet4BindAddress string `json:"inet4_bind_address"`
	Inet6BindAddress string `json:"inet6_bind_address"`
	DomainResolver   *struct {
		Server   string `json:"server"`
		Strategy string `json:"strategy"`
	} `json:"domain_resolver"`
}

// assertPinned checks the dial fields of one dialing outbound or endpoint.
func assertPinned(t *testing.T, tag string, entry dialFields, network *desiredstate.Network) {
	t.Helper()
	if entry.BindInterface != network.OutboundInterface {
		t.Fatalf("%s bind_interface = %q", tag, entry.BindInterface)
	}
	want4, want6 := "", ""
	wantStrategy := ""
	if network.OutboundSourceIP == "192.0.2.10" {
		want4, wantStrategy = "192.0.2.10", "ipv4_only"
	}
	if network.OutboundSourceIP == "2001:db8::10" {
		want6, wantStrategy = "2001:db8::10", "ipv6_only"
	}
	if entry.Inet4BindAddress != want4 || entry.Inet6BindAddress != want6 {
		t.Fatalf("%s bind addresses = %q / %q", tag, entry.Inet4BindAddress, entry.Inet6BindAddress)
	}
	if wantStrategy == "" {
		if entry.DomainResolver != nil {
			t.Fatalf("%s pinned a resolver without a source address", tag)
		}
		return
	}
	if entry.DomainResolver == nil || entry.DomainResolver.Server != "_panel_dns_" || entry.DomainResolver.Strategy != wantStrategy {
		t.Fatalf("%s domain_resolver = %+v", tag, entry.DomainResolver)
	}
}

// TestOutboundSourceCoversEveryDialingOutbound asserts the shape of the pinning
// independently of the golden bytes: every outbound that dials and every
// outbound WireGuard endpoint is pinned, while block, the group outbounds and
// the inbound WireGuard endpoint are not.
func TestOutboundSourceCoversEveryDialingOutbound(t *testing.T) {
	for _, scenario := range sourceScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			rendered, err := renderWithOptions(context.Background(), scenario.state, goldenAt, scenario.options)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Outbounds []dialFields `json:"outbounds"`
				Endpoints []dialFields `json:"endpoints"`
			}
			if err := json.Unmarshal(rendered.Data, &document); err != nil {
				t.Fatal(err)
			}
			network := scenario.state.Network
			dialing := 0
			for _, outbound := range document.Outbounds {
				if !dialable(outbound.Type) {
					if outbound.BindInterface != "" || outbound.Inet4BindAddress != "" || outbound.Inet6BindAddress != "" || outbound.DomainResolver != nil {
						t.Fatalf("outbound %s (%s) was pinned", outbound.Tag, outbound.Type)
					}
					continue
				}
				dialing++
				assertPinned(t, outbound.Tag, outbound, network)
			}
			for _, endpoint := range document.Endpoints {
				if endpoint.Tag != scenario.endpoint {
					if endpoint.BindInterface != "" || endpoint.Inet4BindAddress != "" || endpoint.Inet6BindAddress != "" || endpoint.DomainResolver != nil {
						t.Fatalf("inbound endpoint %s was pinned", endpoint.Tag)
					}
					continue
				}
				assertPinned(t, endpoint.Tag, endpoint, network)
			}
			if dialing < 3 {
				t.Fatalf("only %d dialing outbounds rendered", dialing)
			}
		})
	}
}
func TestGoldenOutboundSourceRenderedConfigurations(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	for _, scenario := range sourceScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			rendered, err := renderWithOptions(context.Background(), scenario.state, goldenAt, scenario.options)
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
