package desiredstate

import (
	"encoding/json"
	"testing"
)

func valid() State {
	return State{SchemaVersion: 1, Revision: 1, NodeID: "node-a", Logging: Logging{DomainOnly: true}, Inbounds: []Inbound{{ID: "socks", Enabled: true, Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, DefaultOutbound: "direct"}}, Outbounds: []Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
}
func TestStateRejectsUnsafeAndInvalidConfig(t *testing.T) {
	state := valid()
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(`{"schema_version":1,"arbitrary_core_json":{}}`)); err == nil {
		t.Fatal("accepted unknown configuration")
	}
	mutations := []func(*State){func(s *State) { s.SchemaVersion = 2 }, func(s *State) { s.Logging.DomainOnly = false }, func(s *State) { s.NodeID = "../escape" }, func(s *State) { s.Inbounds[0].Listen = "0.0.0.0" }, func(s *State) { s.Inbounds[0].DefaultOutbound = "missing" }, func(s *State) {
		s.Inbounds = append(s.Inbounds, Inbound{ID: "other", Enabled: true, Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, DefaultOutbound: "direct"})
	}, func(s *State) { s.ProxyChains = []ProxyChain{{ID: "chain", NodeIDs: []string{"a", "a"}}} }, func(s *State) { s.ProxyChains = []ProxyChain{{ID: "chain", NodeIDs: []string{"a", "b", "c", "d"}}} }}
	for index, mutate := range mutations {
		state := valid()
		mutate(&state)
		if state.Validate() == nil {
			t.Fatalf("accepted invalid case %d", index)
		}
	}
}

func TestStateToleratesRemovedBudgetAndConnectionAllowanceKeys(t *testing.T) {
	data := []byte(`{"schema_version":1,"revision":1,"node_id":"node-a","logging":{"domain_only":true},"users":[{"id":"user","enabled":true,"budget":{"id":"grant","node_id":"node-a","bytes":100,"sequence":1},"connection_allowance":{"id":"slots","node_id":"node-a","slots":4,"sequence":1}}],"outbounds":[{"id":"direct","protocol":"direct","enabled":true}]}`)
	state, err := Decode(data)
	if err != nil {
		t.Fatalf("legacy budget keys rejected: %v", err)
	}
	if len(state.Users) != 1 || state.Users[0].ID != "user" || !state.Users[0].Enabled {
		t.Fatalf("legacy keys changed user decoding: %+v", state.Users)
	}
	rogue := []byte(`{"schema_version":1,"revision":1,"node_id":"node-a","logging":{"domain_only":true},"users":[{"id":"user","enabled":true,"rogue":true}],"outbounds":[{"id":"direct","protocol":"direct","enabled":true}]}`)
	if _, err := Decode(rogue); err == nil {
		t.Fatal("unknown user field accepted")
	}
}

// A state that does not configure the outbound network keeps exactly the bytes
// it had before the network field existed: an older Agent rejects an unknown
// field, so a revision carrying one would be refused instead of applied.
func TestStateWithoutNetworkKeepsPreFeatureBytes(t *testing.T) {
	const pinned = `{"schema_version":1,"revision":1,"node_id":"node-a","users":null,"credentials":null,"inbounds":[{"id":"socks","protocol":"socks5","listen":"127.0.0.1","port":1080,"enabled":true,"credential_ids":null,"default_outbound":"direct","subscription_visible":false,"settings":{}}],"outbounds":[{"id":"direct","protocol":"direct","enabled":true,"settings":{}}],"outbound_groups":null,"proxy_chains":null,"policies":null,"logging":{"domain_only":true}}`
	data, err := json.Marshal(valid())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != pinned {
		t.Fatalf("serialized state changed:\n got %s\nwant %s", data, pinned)
	}
	state, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if state.Network != nil {
		t.Fatal("network block appeared on a node that does not use the feature")
	}
}

func TestDecodeRemapsRemovedWeightedStrategy(t *testing.T) {
	data := []byte(`{"schema_version":1,"revision":7,"node_id":"node-a","logging":{"domain_only":true},"inbounds":[{"id":"socks","enabled":true,"protocol":"socks5","listen":"127.0.0.1","port":1080,"default_outbound":"pool"}],"outbounds":[{"id":"direct","protocol":"direct","enabled":true}],"outbound_groups":[{"id":"pool","strategy":"weighted","probe_url":"https://probe.example.test/health","probe_interval":30,"failover":"Block","members":[{"outbound_id":"direct","priority":1,"weight":3}]}]}`)
	state, err := Decode(data)
	if err != nil {
		t.Fatalf("stored weighted revision rejected: %v", err)
	}
	if len(state.OutboundGroups) != 1 || state.OutboundGroups[0].Strategy != "round_robin" {
		t.Fatalf("stored weighted revision not remapped: %+v", state.OutboundGroups)
	}
	// A new state that still asks for weighted is refused.
	fresh := valid()
	fresh.OutboundGroups = []OutboundGroup{{ID: "pool", Strategy: "weighted", ProbeURL: "https://probe.example.test/health", ProbeInterval: 30, Failover: "Block", Members: []GroupMember{{OutboundID: "direct", Priority: 1, Weight: 1}}}}
	if err := fresh.Validate(); err == nil {
		t.Fatal("new weighted group accepted")
	}
}
