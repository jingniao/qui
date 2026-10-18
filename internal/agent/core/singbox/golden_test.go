package singbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// goldenAt is the fixed instant every golden scenario renders at, so policy
// time windows and ordering never depend on the wall clock.
var goldenAt = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// goldenOptions render the private control surface too, because the loopback
// Clash API and the reserved-port guard rule are part of what step 6.2 pins
// down. The port and secret are fixed so the files are stable.
var goldenOptions = renderOptions{statsPort: 15402, statsSecret: strings.Repeat("b", 64)}

type goldenScenario struct {
	name  string
	state desiredstate.State
}

func goldenScenarios() []goldenScenario {
	base := func(protocol string) desiredstate.State {
		secret := "golden-socks-secret"
		if protocol == "vless" {
			secret = "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"
		}
		return desiredstate.State{
			SchemaVersion: 1, Revision: 1, NodeID: "golden-node", Logging: desiredstate.Logging{DomainOnly: true},
			Users:       []desiredstate.User{{ID: "user", Enabled: true}},
			Credentials: []desiredstate.Credential{{ID: "device-credential", UserID: "user", DeviceID: "device", Protocol: protocol, Username: "golden-user", Secret: secret}},
			Inbounds:    []desiredstate.Inbound{{ID: "entry", Protocol: protocol, Listen: "127.0.0.1", Port: 1080, Enabled: true, CredentialIDs: []string{"device-credential"}, DefaultOutbound: "direct"}},
			Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}},
		}
	}
	socks := base("socks5")
	vless := base("vless")
	relay := base("vless")
	relay.Credentials = append(relay.Credentials, desiredstate.Credential{ID: "relay-credential", Protocol: "trojan", Internal: true, Secret: "independent-relay-password"})
	relay.Outbounds = append(relay.Outbounds, desiredstate.Outbound{ID: "to-exit", Protocol: "trojan", Address: "198.51.100.10", Port: 443, Enabled: true, CredentialID: "relay-credential", TargetNodeID: "exit-node", Settings: desiredstate.ProtocolSettings{Security: "tls", ServerName: "exit.example.test"}})
	relay.Inbounds[0].DefaultOutbound = "to-exit"
	blocked := base("socks5")
	blocked.Policies = []desiredstate.Policy{{ID: "deny-social", Name: "deny social", Enabled: true, Priority: 10, Match: desiredstate.Match{Suffixes: []string{"blocked.example"}}, Action: desiredstate.Action{Type: "Block"}}}
	grouped := base("socks5")
	grouped.Outbounds = append(grouped.Outbounds,
		desiredstate.Outbound{ID: "egress-primary", Protocol: "http", Address: "198.51.100.20", Port: 8080, Enabled: true},
		desiredstate.Outbound{ID: "egress-backup", Protocol: "socks5", Address: "198.51.100.21", Port: 1080, Enabled: true})
	grouped.OutboundGroups = []desiredstate.OutboundGroup{{ID: "egress-pool", Strategy: "primary", ProbeURL: "https://probe.example.test/generate_204", ProbeInterval: 30, Failover: "BackupOutbound", BackupTargetID: "egress-backup", Members: []desiredstate.GroupMember{{OutboundID: "egress-primary", Priority: 1, Weight: 1}, {OutboundID: "egress-backup", Priority: 2, Weight: 1}}}}
	grouped.Inbounds[0].DefaultOutbound = "group:egress-pool"
	return []goldenScenario{
		{name: "socks5-direct", state: socks},
		{name: "vless-direct", state: vless},
		{name: "vless-trojan", state: relay},
		{name: "block-rule", state: blocked},
		{name: "group-primary-backup", state: grouped},
	}
}

func TestGoldenRenderedConfigurations(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	for _, scenario := range goldenScenarios() {
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
			// The fork is gone: no rendered configuration may carry a fork key
			// or type, in any casing.
			if strings.Contains(strings.ToLower(string(rendered.Data)), "proxycontrol") {
				t.Fatalf("%s contains a fork key", path)
			}
			var document map[string]any
			if err := json.Unmarshal(rendered.Data, &document); err != nil {
				t.Fatal(err)
			}
			experimental, _ := document["experimental"].(map[string]any)
			if _, ok := experimental["proxycontrol_connections"]; ok {
				t.Fatal("fork connection options rendered")
			}
			if _, ok := experimental["v2ray_api"]; ok {
				t.Fatal("v2ray API rendered on an official core")
			}
			if _, ok := experimental["clash_api"]; !ok {
				t.Fatal("clash API missing from the golden")
			}
			if binary == "" {
				return
			}
			// With a real core configured the golden itself must pass native
			// validation, so a fork-only construct can never be pinned again.
			validator := &Adapter{Path: binary, Directory: t.TempDir()}
			if err := validator.Validate(context.Background(), core.RenderedConfig{Data: golden}); err != nil {
				t.Fatalf("official core rejected the golden: %v", err)
			}
		})
	}
}
