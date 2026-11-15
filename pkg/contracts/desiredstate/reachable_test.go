package desiredstate

import (
	"reflect"
	"testing"
)

func TestReachableOutboundsRoutingGraph(t *testing.T) {
	s := State{
		Inbounds:       []Inbound{{ID: "entry", Enabled: true, DefaultOutbound: "group:g", CredentialIDs: []string{"user"}}, {ID: "disabled", DefaultOutbound: "unreachable", CredentialIDs: []string{"other"}}, {ID: "gost", Core: CoreGost, Enabled: true, DefaultOutbound: "gost-only"}},
		Credentials:    []Credential{{ID: "user", UserID: "u"}, {ID: "other", UserID: "other"}},
		OutboundGroups: []OutboundGroup{{ID: "g", Members: []GroupMember{{OutboundID: "first"}}, BackupTargetID: "chain:c"}},
		ProxyChains:    []ProxyChain{{ID: "c", OutboundIDs: []string{"hop"}, FinalOutboundID: "final", BackupTargetID: "backup"}},
		Policies:       []Policy{{ID: "p", Enabled: true, Action: Action{TargetID: "policy", BackupID: "policy-backup"}}, {ID: "other", Enabled: true, Scope: Scope{UserIDs: []string{"other"}}, Action: Action{TargetID: "unreachable"}}},
	}
	want := []string{"first", "hop", "final", "backup", "policy", "policy-backup", "out-backup"}
	for _, id := range append(append([]string{}, want...), "unreachable", "gost-only") {
		s.Outbounds = append(s.Outbounds, Outbound{ID: id, Enabled: true})
	}
	s.Outbounds[0].BackupTargetID = "out-backup"
	var got []string
	for _, out := range s.ReachableOutbounds() {
		got = append(got, out.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reachable %v, want %v", got, want)
	}
	s.Inbounds[0].Enabled = false
	if got := s.ReachableOutbounds(); len(got) != 0 {
		t.Fatalf("disabled roots reached %v", got)
	}
}
