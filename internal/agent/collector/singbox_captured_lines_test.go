package collector

import (
	"proxycontrol/pkg/contracts/access"
	"testing"
	"time"
)

// TestCapturedSingboxCoreLinesBecomeAccessRollups pins the sing-box source
// against unmodified core log lines of the official release: one rejected
// connection and one allowed connection, with the interleaved core noise that
// must never become a record.
func TestCapturedSingboxCoreLinesBecomeAccessRollups(t *testing.T) {
	context := &Context{NodeID: "node", Revision: 7, Inbounds: map[string]Inbound{
		"entry": {Protocol: "socks5", Accounts: map[string]Account{"u1": {ID: "credential", UserID: "user", DeviceID: "device"}, "credential": {ID: "credential", UserID: "user", DeviceID: "device"}}},
	}, Exits: map[string]Exit{
		SingboxRuleTag(0): {ID: "block", Protocol: "block", RuleID: "deny-blocked-example", LogDomain: true},
		SingboxRuleTag(1): {ID: "direct", Protocol: "direct", RuleID: "allow-rest", LogDomain: true},
	}}
	records := []access.Rollup{}
	rejects := 0
	writer := NewSingboxWriter(context, func(record access.Rollup) error { records = append(records, record); return nil }, func() { rejects++ })
	at := time.Date(2026, 10, 17, 11, 34, 0, 0, time.UTC)
	writer.now = func() time.Time { return at }
	for _, line := range capturedLines(t, "singbox-core-lines.txt") {
		writer.Write([]byte(line + "\n"))
	}
	if rejects != 0 {
		t.Fatalf("captured core noise rejected: %d", rejects)
	}
	if len(records) != 2 {
		t.Fatalf("captured connections not recorded: %+v", records)
	}
	blocked := records[0]
	if blocked.Allowed || blocked.Action != "Block" || blocked.RuleID != "deny-blocked-example" || blocked.Domain != "blocked.example" || blocked.TargetPort != 80 || blocked.UserID != "user" || blocked.DeviceID != "device" || blocked.InboundProtocol != "socks5" || !blocked.Bucket.Equal(at) {
		t.Fatalf("rejected connection not attributed: %+v", blocked)
	}
	allowed := records[1]
	if !allowed.Allowed || allowed.Action != "Direct" || allowed.RuleID != "allow-rest" || allowed.Domain != "localhost" || allowed.UserID != "user" || allowed.TrafficKnown {
		t.Fatalf("allowed connection not attributed: %+v", allowed)
	}
}
