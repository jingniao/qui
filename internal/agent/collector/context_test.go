package collector

import (
	"encoding/json"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
	"time"
)

func TestContextIdentityPrivacyAndInternalProbeExclusion(t *testing.T) {
	state := desiredstate.State{NodeID: "node", Revision: 1, Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "username", Secret: "PRIVATE-CREDENTIAL"}, {ID: "internal", Protocol: "trojan", Internal: true, Secret: "PRIVATE-RELAY"}}, Inbounds: []desiredstate.Inbound{{ID: "user-in", Enabled: true, Protocol: "socks5", CredentialIDs: []string{"credential"}}, {ID: "relay-in", Enabled: true, Protocol: "trojan", CredentialIDs: []string{"internal"}}}}
	context := NewContext(state)
	context.Exits["actual-rule-tag"] = Exit{ID: "block", Protocol: "block", RuleID: "rule", AccountID: "credential", LogDomain: true}
	encoded, _ := json.Marshal(context)
	if strings.Contains(string(encoded), "PRIVATE-") {
		t.Fatal("secret copied into telemetry context")
	}
	event := XrayEvent{InboundID: "user-in", OutboundTag: "actual-rule-tag", Domain: "blocked.test", Port: 443, Network: "tcp"}
	record, include, err := context.Record(event, time.Now())
	if err != nil || !include || record.UserID != "user" || record.DeviceID != "device" || record.RuleID != "rule" || record.Allowed {
		t.Fatal(record, include, err)
	}
	event.InboundID = "relay-in"
	if _, include, err := context.Record(event, time.Now()); err != nil || include {
		t.Fatal("internal relay duplicated user access")
	}
	event.InboundID = "_probe_out"
	if _, include, err := context.Record(event, time.Now()); err != nil || include {
		t.Fatal("health probe audited as user")
	}
	event.InboundID = "user-in"
	context.Exits["actual-rule-tag"] = Exit{LogDomain: false}
	if _, include, err := context.Record(event, time.Now()); err != nil || include {
		t.Fatal("disabled domain logging ignored")
	}
}

func TestWriterFragmentedLinesOversizeAndRawContentNotPublished(t *testing.T) {
	context := &Context{NodeID: "node", Revision: 1, Inbounds: map[string]Inbound{"in": {Protocol: "socks5", Accounts: map[string]Account{}}}, Exits: map[string]Exit{"direct": {ID: "direct", Protocol: "direct", LogDomain: true}}}
	records := []access.Rollup{}
	rejected := 0
	writer := NewWriter(context, func(record access.Rollup) error { records = append(records, record); return nil }, func() { rejected++ })
	writer.Write([]byte("from 192.0.2.1:3000 accepted tcp:example.test:443 [in -> "))
	writer.Write([]byte("direct]\nPRIVATE-ERROR password\n"))
	writer.Write([]byte(strings.Repeat("x", 9000) + "\n"))
	if len(records) != 1 || records[0].Domain != "example.test" || rejected != 1 {
		t.Fatal(records, rejected)
	}
	// A rejection the core recorded without a destination (a failed inbound
	// handshake) has no domain or address to attribute: it is counted as an
	// unattributed observation and never becomes a record.
	writer.Write([]byte("2026/10/17 11:33:32.346148 from tcp:127.0.0.1:63178 rejected  proxy/socks: invalid username or password\n"))
	if len(records) != 1 || rejected != 2 {
		t.Fatal(records, rejected)
	}
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), "192.0.2.1") {
		t.Fatal("raw log/source leaked")
	}
}
