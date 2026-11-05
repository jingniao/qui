package desiredstate

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCoreRoutingClosure(t *testing.T) {
	s := State{NodeID: "n", SchemaVersion: 1, Revision: 1, Logging: Logging{DomainOnly: true},
		Inbounds:    []Inbound{{ID: "a", Enabled: true, DefaultOutbound: "chain:chain", CredentialIDs: []string{"ca"}}, {ID: "b", Core: "sing-box", Enabled: true, DefaultOutbound: "ob", CredentialIDs: []string{"cb"}}},
		Credentials: []Credential{{ID: "ca", UserID: "ua"}, {ID: "cb", UserID: "ub"}, {ID: "relay", Internal: true}}, Users: []User{{ID: "ua"}, {ID: "ub"}},
		Outbounds:      []Outbound{{ID: "oa", BackupTargetID: "fallback", CredentialID: "relay"}, {ID: "fallback"}, {ID: "ob"}, {ID: "unused"}},
		OutboundGroups: []OutboundGroup{{ID: "group", Members: []GroupMember{{OutboundID: "oa"}}}, {ID: "unused-group"}},
		ProxyChains:    []ProxyChain{{ID: "chain", OutboundIDs: []string{"group:group"}, FinalOutboundID: "fallback"}},
		Policies:       []Policy{{ID: "global", Enabled: true, Action: Action{TargetID: "fallback"}}, {ID: "other-user", Scope: Scope{UserIDs: []string{"ub"}}, Action: Action{TargetID: "unused"}}}}
	before, _ := json.Marshal(s)
	a := s.ForCore("xray", "xray")
	ids := []string{}
	for _, v := range a.Outbounds {
		ids = append(ids, v.ID)
	}
	if !reflect.DeepEqual(ids, []string{"oa", "fallback"}) || len(a.Inbounds) != 1 || len(a.OutboundGroups) != 1 || len(a.ProxyChains) != 1 || len(a.Policies) != 1 || len(a.Users) != 1 || len(a.Credentials) != 2 {
		t.Fatalf("unexpected closure: %+v", a)
	}
	b := s.ForCore("sing-box", "xray")
	if len(b.Inbounds) != 1 || b.Inbounds[0].ID != "b" || len(b.ProxyChains) != 0 || len(b.OutboundGroups) != 0 {
		t.Fatalf("cross-core objects leaked: %+v", b)
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("closure mutated desired state")
	}
}

func TestInboundCoreJSONCompatibility(t *testing.T) {
	legacy, _ := json.Marshal(Inbound{ID: "a"})
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(legacy, &fields)
	if _, ok := fields["core"]; ok {
		t.Fatal("empty core changed legacy bytes")
	}
	var inbound Inbound
	if err := json.Unmarshal([]byte(`{"id":"a","core":"sing-box"}`), &inbound); err != nil || inbound.Core != "sing-box" {
		t.Fatal(inbound, err)
	}
}

// TestGostCoreKeepsForwardsAndRefusesOtherFeatures pins the two shared rules of the
// third core kind: a global (unscoped) policy never attaches to a GOST forward,
// because GOST has no routing surface, and a non-forward inbound may not be assigned
// to it at all.
func TestGostCoreKeepsForwardsAndRefusesOtherFeatures(t *testing.T) {
	s := State{SchemaVersion: 1, Revision: 1, NodeID: "n", Logging: Logging{DomainOnly: true},
		Inbounds: []Inbound{
			{ID: "forward", Core: CoreGost, Protocol: "forward", Listen: "127.0.0.1", Port: 20000, Enabled: true, DefaultOutbound: "direct", Settings: ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp,udp"}}},
			{ID: "user", Core: CoreXray, Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct"},
		},
		Outbounds: []Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
		Policies:  []Policy{{ID: "global", Enabled: true, Action: Action{Type: "direct"}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	gost := s.ForCore(CoreGost, CoreXray)
	if len(gost.Inbounds) != 1 || gost.Inbounds[0].ID != "forward" || len(gost.Policies) != 0 {
		t.Fatalf("gost closure: %+v", gost)
	}
	// The same unscoped policy does reach a user-serving core.
	if other := s.ForCore(CoreXray, CoreXray); len(other.Policies) != 1 {
		t.Fatalf("xray closure lost the global policy: %+v", other.Policies)
	}
	broken := s
	broken.Inbounds = []Inbound{{ID: "user", Core: CoreGost, Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct"}}
	if err := broken.Validate(); err == nil {
		t.Fatal("a GOST inbound accepted a user protocol")
	}
	unknown := s
	unknown.Inbounds = []Inbound{{ID: "forward", Core: "shell", Protocol: "forward", Listen: "127.0.0.1", Port: 20000, Enabled: true, DefaultOutbound: "direct", Settings: ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp"}}}}
	if err := unknown.Validate(); err == nil {
		t.Fatal("an unknown inbound core was accepted")
	}
	if !CoreSupportsInbound(CoreGost, "forward") || CoreSupportsOutbound(CoreGost, "forward") || CoreSupportsInbound(CoreGost, "socks5") {
		t.Fatal("gost support row is not forward-only")
	}
	if GostContractVersion != 4 {
		t.Fatalf("X0 reports contract version %d", GostContractVersion)
	}
	if err := ValidateForwardCore(CoreXray, ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp", MaxFails: gostInt(1)}}); err == nil {
		t.Fatal("an xray forward accepted a GOST-only option")
	}
	if err := ValidateForwardCore(CoreGost, ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp", MaxFails: gostInt(1)}}); err != nil {
		t.Fatal(err)
	}
}

// gostInt is the smallest way to write an explicitly configured optional integer.
func gostInt(value int) *int { return &value }

// A GOST forward leaves through its own targets; any default other than the
// enabled direct outbound is refused before the Agent renders it.
func TestGostForwardRequiresTheDirectDefault(t *testing.T) {
	s := State{SchemaVersion: 1, Revision: 1, NodeID: "n", Logging: Logging{DomainOnly: true},
		Inbounds:  []Inbound{{ID: "forward", Core: CoreGost, Protocol: "forward", Listen: "127.0.0.1", Port: 20000, Enabled: true, DefaultOutbound: "block", Settings: ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp"}}}},
		Outbounds: []Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("a GOST forward accepted the block default")
	}
	s.Inbounds[0].DefaultOutbound = "direct"
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
