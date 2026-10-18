package desiredstate

import "testing"

func TestFallbackKindReferencesCyclesAndRemoteChainScope(t *testing.T) {
	state := State{NodeID: "a", Outbounds: []Outbound{{ID: "one", Enabled: true, FailurePolicy: "BackupOutboundGroup", BackupTargetID: "group"}, {ID: "two", Enabled: true}}, OutboundGroups: []OutboundGroup{{ID: "group", Members: []GroupMember{{OutboundID: "two", Weight: 1}}, Failover: "BackupProxyChain", BackupTargetID: "chain"}}, ProxyChains: []ProxyChain{{ID: "chain", NodeIDs: []string{"a", "b"}, OutboundIDs: []string{"two"}, Failover: "Block"}}}
	if err := ValidateScheduling(state); err != nil {
		t.Fatal(err)
	}
	state.ProxyChains[0].Failover = "BackupOutbound"
	state.ProxyChains[0].BackupTargetID = "one"
	if err := ValidateScheduling(state); err == nil {
		t.Fatal("mixed group/chain/outbound cycle accepted")
	}
	state.ProxyChains[0].Failover = "Block"
	state.Outbounds[0].BackupTargetID = "chain"
	if err := ValidateScheduling(state); err == nil {
		t.Fatal("wrong backup kind accepted")
	}
	state.Outbounds[0].BackupTargetID = "missing"
	if err := ValidateScheduling(state); err == nil {
		t.Fatal("missing backup accepted")
	}
	state.Outbounds[0].FailurePolicy = "Block"
	state.ProxyChains = append(state.ProxyChains, ProxyChain{ID: "remote", NodeIDs: []string{"b", "c"}, OutboundIDs: []string{"remote-out"}, Failover: "BackupOutboundGroup", BackupTargetID: "remote-group"})
	if err := ValidateScheduling(state); err != nil {
		t.Fatal("another node's chain references required locally", err)
	}
}
