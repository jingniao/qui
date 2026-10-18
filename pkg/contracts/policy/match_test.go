package policy

import (
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func rule() desiredstate.Policy {
	return desiredstate.Policy{ID: "rule-1", Name: "Test policy", Priority: 10, Enabled: true, Action: desiredstate.Action{Type: "Block"}, Match: desiredstate.Match{Suffixes: []string{"example.com"}}}
}
func request() Request {
	return Request{NodeID: "node-a", UserID: "user-a", DeviceID: "device-a", GroupIDs: []string{"group-a"}, Domain: "api.example.com", TargetIP: "192.0.2.25", Port: 443, Network: "tcp", Time: time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)}
}
func TestScopeAddressPortNetworkAndPrioritySemantics(t *testing.T) {
	policy := rule()
	policy.Scope = desiredstate.Scope{UserIDs: []string{"user-a"}, DeviceIDs: []string{"device-a"}, NodeIDs: []string{"node-a"}, GroupIDs: []string{"group-a"}}
	policy.Match.Ports = []string{"443", "8000-9000"}
	policy.Match.Network = "tcp"
	if err := Validate(&policy); err != nil {
		t.Fatal(err)
	}
	r := request()
	if !Matches(policy, r) {
		t.Fatal("matching request rejected")
	}
	changes := []func(*Request){func(r *Request) { r.UserID = "other" }, func(r *Request) { r.DeviceID = "other" }, func(r *Request) { r.NodeID = "other" }, func(r *Request) { r.GroupIDs = []string{"other"} }, func(r *Request) { r.Domain = "evil-example.com" }, func(r *Request) { r.Port = 80 }, func(r *Request) { r.Network = "udp" }}
	for index, change := range changes {
		r := request()
		change(&r)
		if Matches(policy, r) {
			t.Fatalf("scope or condition bypass %d", index)
		}
	}
	policy.Match.CIDRs = []string{"192.0.2.0/24"}
	r = request()
	r.Domain = "unrelated.test"
	if !Matches(policy, r) {
		t.Fatal("IP alternative did not match")
	}
	r.Port = 8080
	if !Matches(policy, r) {
		t.Fatal("port range did not match")
	}
	early := policy
	early.ID = "first"
	early.Priority = 1
	early.Action.Type = "Direct"
	disabled := early
	disabled.ID = "disabled"
	disabled.Priority = 0
	disabled.Enabled = false
	decision := Simulate([]desiredstate.Policy{policy, disabled, early}, r, desiredstate.Action{Type: "Block"})
	if !decision.Matched || decision.PolicyID != "first" || decision.Action.Type != "Direct" {
		t.Fatal("first enabled priority did not win")
	}
	r.TargetIP = "198.51.100.1"
	decision = Simulate([]desiredstate.Policy{policy}, r, desiredstate.Action{Type: "Direct"})
	if decision.Matched || decision.Action.Type != "Direct" {
		t.Fatal("default rule wrong")
	}
}
func TestDomainsWildcardIDNAAndCIDRs(t *testing.T) {
	policy := rule()
	policy.Match = desiredstate.Match{Domains: []string{"*.Example.COM", "EXACT.test."}, CIDRs: []string{"2001:db8::/32"}}
	if err := Validate(&policy); err != nil {
		t.Fatal(err)
	}
	r := request()
	if !Matches(policy, r) {
		t.Fatal("wildcard did not match")
	}
	r.Domain = "example.com"
	r.TargetIP = "198.51.100.1"
	if Matches(policy, r) {
		t.Fatal("wildcard unexpectedly matched bare suffix")
	}
	r.Domain = "exact.test"
	if !Matches(policy, r) {
		t.Fatal("exact normalized domain failed")
	}
	r.Domain = "other.test"
	r.TargetIP = "2001:db8::1"
	if !Matches(policy, r) {
		t.Fatal("IPv6 CIDR failed")
	}
	domain, err := Domain("例子.测试", false)
	if err != nil || domain != "xn--fsqu00a.xn--0zwm56d" {
		t.Fatal("IDNA normalization failed", domain, err)
	}
	for _, value := range []string{"https://example.com/path", "example.com?password=value", "user@example.com", "bad domain.com", "example.com:443", "regexp:.*"} {
		if _, err := Domain(value, true); err == nil {
			t.Fatal("URL or raw regexp accepted", value)
		}
	}
}
func TestTimeWindowsUsePolicyZoneAndSupportMidnight(t *testing.T) {
	policy := rule()
	policy.Timezone = "Asia/Shanghai"
	policy.Match.TimeRange = "23:00-06:00"
	if err := Validate(&policy); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		hour, minute int
		want         bool
	}{{14, 59, false}, {15, 0, true}, {20, 0, true}, {21, 59, true}, {22, 0, false}} {
		at := time.Date(2026, 10, 1, test.hour, test.minute, 0, 0, time.UTC)
		if got := TimeActive(policy, at); got != test.want {
			t.Fatalf("%v active=%v want=%v", at, got, test.want)
		}
	}
	policy.Timezone = "America/Los_Angeles"
	policy.Match.TimeRange = "09:00-10:00"
	if !TimeActive(policy, time.Date(2026, 7, 1, 16, 30, 0, 0, time.UTC)) || !TimeActive(policy, time.Date(2026, 1, 1, 17, 30, 0, 0, time.UTC)) {
		t.Fatal("daylight saving offset ignored")
	}
	for _, value := range []string{"24:00-25:00", "09:00-09:00", "9:00-10:00", "10:60-11:00"} {
		policy.Match.TimeRange = value
		if Validate(&policy) == nil {
			t.Fatal("invalid time window accepted", value)
		}
	}
}
