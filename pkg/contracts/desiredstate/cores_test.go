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
