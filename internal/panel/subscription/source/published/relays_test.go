package published

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

type relayFixture struct {
	state      desiredstate.State
	credential user.Credential
	protocol   string
	relays     []Relay
	groups     []string
}

func (f *relayFixture) Credentials(context.Context, string, bool) ([]user.Credential, error) {
	return []user.Credential{f.credential}, nil
}
func (f *relayFixture) ExpandedNodes(context.Context, []string) ([]node.Node, error) {
	return []node.Node{{ID: "destination", Name: "US", PublicIP: "destination.example", Capabilities: &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: f.protocol, Inbound: true}}}}}, nil
}
func (f *relayFixture) UserInboundGroups(context.Context, string, string) ([]string, error) {
	return f.groups, nil
}
func (f *relayFixture) Published(context.Context, string) (desiredstate.State, error) {
	return f.state, nil
}
func (f *relayFixture) Relays(context.Context) ([]Relay, error) { return f.relays, nil }
func newRelayFixture(protocol string, settings desiredstate.ProtocolSettings) *relayFixture {
	return &relayFixture{protocol: protocol, groups: []string{"group"}, credential: user.Credential{ID: "credential", Protocol: protocol, Status: "active", Username: "alice", Secret: "secret"}, state: desiredstate.State{NodeID: "destination", Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "identity", Protocol: protocol, Username: "alice", Secret: "secret"}}, Inbounds: []desiredstate.Inbound{{ID: "inbound", Protocol: protocol, Listen: "0.0.0.0", Port: 443, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{"credential"}, Settings: settings}}}, relays: []Relay{{ID: "rule", InboundID: "inbound", Entry: proxy.Ref{ID: "entry", Name: "HK"}, Server: "203.0.113.1", Port: 31000, DestinationAddress: "destination.example", DestinationPort: 443, Network: "both", Enabled: true, Visible: true}}}
}
func relayProxies(f *relayFixture) ([]proxy.Proxy, error) {
	s := &Source{Users: f, Nodes: f, Published: f, Relays: f}
	p, _, err := s.Proxies(context.Background(), source.Viewer{UserID: "user", DeviceID: "identity", NodeGroupIDs: []string{"group"}, AllowedProtocols: []string{f.protocol}})
	return p, err
}
func TestRelayedSubscriptionGoldens(t *testing.T) {
	for _, tc := range []struct {
		name, protocol string
		settings       desiredstate.ProtocolSettings
	}{
		{"vless_tls_implicit", "vless", desiredstate.ProtocolSettings{Security: "tls", Transport: "ws", Path: "/proxy", Flow: ""}},
		{"vless_tls", "vless", desiredstate.ProtocolSettings{Security: "tls", ServerName: "tls.example", Flow: "xtls-rprx-vision"}},
		{"trojan", "trojan", desiredstate.ProtocolSettings{Security: "tls", ServerName: "tls.example"}},
		{"shadowsocks", "shadowsocks", desiredstate.ProtocolSettings{Method: "aes-128-gcm"}},
		{"hysteria2", "hysteria2", desiredstate.ProtocolSettings{Security: "tls", ServerName: "tls.example"}},
		{"socks5", "socks5", desiredstate.ProtocolSettings{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRelayFixture(tc.protocol, tc.settings)
			endpoints, err := relayProxies(f)
			if err != nil || len(endpoints) != 2 {
				t.Fatal(endpoints, err)
			}
			direct, relay := endpoints[0], endpoints[1]
			if direct.Server != "destination.example" || relay.Server != "203.0.113.1" || relay.Port != 31000 || relay.Name != "US · "+tc.protocol+" · 经 HK" || !reflect.DeepEqual(relay.Credential, direct.Credential) || !reflect.DeepEqual(relay.Transport, direct.Transport) || relay.Flow != direct.Flow {
				t.Fatal(endpoints)
			}
			outputs := map[string]string{}
			for _, exporter := range []format.Exporter{uri.URI{}, uri.Base64{}, singbox.Exporter{}, clash.Meta{}, clash.Legacy{}} {
				out, err := exporter.Render(format.Profile{Proxies: endpoints})
				if err != nil {
					t.Fatal(exporter.ID(), err)
				}
				outputs[exporter.ID()] = string(out.Body)
			}
			data, err := json.MarshalIndent(outputs, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n')
			path := filepath.Join("testdata", tc.name+".json")
			if os.Getenv("UPDATE_RELAY_GOLDENS") == "1" {
				if err = os.MkdirAll("testdata", 0755); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != string(data) {
				t.Fatalf("golden mismatch %s\ngot %s", path, data)
			}
		})
	}
}
func TestRelayedSubscriptionFilters(t *testing.T) {
	for _, protocol := range []string{"vless", "trojan", "shadowsocks", "socks5", "hysteria2", "tuic"} {
		for _, network := range []string{"tcp", "udp", "both"} {
			f := newRelayFixture(protocol, desiredstate.ProtocolSettings{Security: "tls"})
			f.relays[0].Network = network
			p, err := relayProxies(f)
			if err != nil {
				t.Fatal(err)
			}
			udp := protocol == "hysteria2" || protocol == "tuic"
			want := 1
			if network == "both" || network == "udp" && udp || network == "tcp" && !udp {
				want = 2
			}
			if len(p) != want {
				t.Fatalf("%s %s: %d", protocol, network, len(p))
			}
		}
	}
	for _, change := range []func(*relayFixture){
		func(f *relayFixture) { f.relays[0].Visible = false },
		func(f *relayFixture) { f.relays[0].Enabled = false },
		func(f *relayFixture) { f.relays = nil },
	} {
		f := newRelayFixture("socks5", desiredstate.ProtocolSettings{})
		change(f)
		p, err := relayProxies(f)
		if err != nil || len(p) != 1 {
			t.Fatal(p, err)
		}
	}
	for _, change := range []func(*relayFixture){
		func(f *relayFixture) { f.groups = nil },
		func(f *relayFixture) { f.state.Credentials[0].Internal = true },
		func(f *relayFixture) { f.state.Credentials[0].Secret = "old" },
		func(f *relayFixture) { f.state.Inbounds[0].SubscriptionVisible = false },
		func(f *relayFixture) { f.state.Inbounds[0].Enabled = false },
	} {
		f := newRelayFixture("socks5", desiredstate.ProtocolSettings{})
		change(f)
		p, err := relayProxies(f)
		if err != nil || len(p) != 0 {
			t.Fatal(p, err)
		}
	}
	f := newRelayFixture("hysteria2", desiredstate.ProtocolSettings{Security: "tls", TrustedCAPEM: "pinned"})
	p, err := relayProxies(f)
	if err != nil {
		t.Fatal(err)
	}
	out, err := (uri.URI{}).Render(format.Profile{Proxies: p})
	if err != nil || len(out.Body) != 0 {
		t.Fatal(out, err)
	}
}
