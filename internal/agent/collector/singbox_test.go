package collector

import (
	"encoding/json"
	"fmt"
	"proxycontrol/pkg/contracts/access"
	"strings"
	"testing"
	"time"
)

func singboxFixture() *Context {
	return &Context{NodeID: "node", Revision: 1, Inbounds: map[string]Inbound{
		"http-in":  {Protocol: "http", Accounts: map[string]Account{"device-login": {ID: "credential", UserID: "user", DeviceID: "device"}, "credential": {ID: "credential", UserID: "user", DeviceID: "device"}}},
		"ss-in":    {Protocol: "shadowsocks", Accounts: map[string]Account{"ss-credential": {ID: "ss-credential", UserID: "user", DeviceID: "ss-device"}}},
		"relay-in": {Protocol: "trojan", Internal: true, Accounts: map[string]Account{"relay": {ID: "relay", Internal: true}}},
		"socks-in": {Protocol: "socks5", Accounts: map[string]Account{"socks-credential": {ID: "socks-credential", UserID: "user", DeviceID: "socks-device"}}},
	}, Exits: map[string]Exit{
		SingboxRuleTag(0): {ID: "block", Protocol: "block", RuleID: "deny", LogDomain: true},
		SingboxRuleTag(1): {ID: "direct", Protocol: "direct", RuleID: "allow", LogDomain: true},
		SingboxRuleTag(2): {ID: "relay-out", Protocol: "trojan", RuleID: "proxy", LogDomain: true},
		SingboxRuleTag(3): {ID: "block", Protocol: "block", RuleID: "private", LogDomain: false},
	}}
}

func nativeLine(id int, level, logger, message string) string {
	return fmt.Sprintf("+0800 2026-10-05 12:00:00 %s [%d 3ms] %s: %s\n", level, id, logger, message)
}

func TestSingboxWriterActualRuleIdentityPrivacyAndInterleavedConnections(t *testing.T) {
	ctx := singboxFixture()
	records := []access.Rollup{}
	rejected := 0
	w := NewSingboxWriter(ctx, func(r access.Rollup) error { records = append(records, r); return nil }, func() { rejected++ })
	w.Write([]byte(nativeLine(1, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to Allowed.Example:443")[:40]))
	w.Write([]byte(nativeLine(1, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to Allowed.Example:443")[40:]))
	w.Write([]byte(nativeLine(2, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to blocked.example:443")))
	w.Write([]byte(nativeLine(2, "DEBUG", "router", "match[0] domain=blocked.example => reject")))
	w.Write([]byte(nativeLine(1, "DEBUG", "router", "match[1] inbound=http-in => route(direct)")))
	w.Write([]byte(nativeLine(1, "INFO", "outbound/direct[direct]", "outbound connection to allowed.example:443")))
	w.Write([]byte(nativeLine(1, "DEBUG", "router", "match[1] => route(direct)")))
	w.Write([]byte(nativeLine(3, "INFO", "inbound/shadowsocks[ss-in]", "inbound packet connection to [::1]:53")))
	w.Write([]byte(nativeLine(3, "DEBUG", "router", "match[2] => route(relay-out)")))
	w.Write([]byte(nativeLine(4, "INFO", "inbound/trojan[relay-in]", "[relay] inbound connection to example.test:443")))
	w.Write([]byte(nativeLine(4, "DEBUG", "router", "match[1] => route(direct)")))
	w.Write([]byte(nativeLine(5, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to private.example:443")))
	w.Write([]byte(nativeLine(5, "DEBUG", "router", "match[3] => reject")))
	w.Write([]byte(nativeLine(6, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to http://host.example/PRIVATE-PATH?token=PRIVATE-TOKEN")))
	w.Write([]byte(nativeLine(6, "DEBUG", "router", "match[1] => route(direct)")))
	w.Write([]byte(nativeLine(7, "ERROR", "connection", "PRIVATE-CONTENT password=PRIVATE-PASSWORD cookie=PRIVATE-COOKIE")))
	if len(records) != 3 || rejected != 1 {
		t.Fatal(records, rejected)
	}
	if records[0].Allowed || records[0].RuleID != "deny" || records[0].DeviceID != "device" {
		t.Fatal(records[0])
	}
	if !records[1].Allowed || records[1].Domain != "allowed.example" || records[1].FinalEgressNodeID != "node" || !records[1].EgressKnown {
		t.Fatal(records[1])
	}
	if records[2].Domain != "" || records[2].TargetIP != "::1" || records[2].DeviceID != "ss-device" || records[2].OutboundProtocol != "trojan" || records[2].EgressKnown {
		t.Fatal(records[2])
	}
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), "PRIVATE-") || strings.Contains(string(encoded), "device-login") {
		t.Fatal("private content or raw login leaked into records")
	}
	for _, record := range records {
		if record.TrafficKnown || record.UploadBytes != 0 || record.DownloadBytes != 0 {
			t.Fatal("account totals were invented as domain bytes")
		}
	}
}

func TestSingboxWriterRejectsUnknownIdentityIndexAndActionMismatch(t *testing.T) {
	for _, fault := range []string{"identity", "index", "action", "collision", "oversize"} {
		t.Run(fault, func(t *testing.T) {
			records := []access.Rollup{}
			rejects := 0
			w := NewSingboxWriter(singboxFixture(), func(r access.Rollup) error { records = append(records, r); return nil }, func() { rejects++ })
			inbound := nativeLine(1, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to example.test:443")
			if fault == "identity" {
				inbound = strings.Replace(inbound, "device-login", "unknown", 1)
			}
			if fault == "oversize" {
				inbound = nativeLine(1, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to "+strings.Repeat("x", 9000)+":443")
			}
			w.Write([]byte(inbound))
			if fault == "collision" {
				w.Write([]byte(nativeLine(1, "INFO", "inbound/shadowsocks[ss-in]", "inbound connection to other.test:443")))
			}
			rule := "match[1] => route(direct)"
			if fault == "index" {
				rule = "match[99] => route(direct)"
			}
			if fault == "action" {
				rule = "match[1] => reject"
			}
			w.Write([]byte(nativeLine(1, "DEBUG", "router", rule)))
			if len(records) != 0 || rejects == 0 {
				t.Fatal("invalid metadata accepted", records, rejects)
			}
		})
	}
}

func TestSingboxUnknownSOCKSUDPDoesNotInventAccountAndMapIsBounded(t *testing.T) {
	records := []access.Rollup{}
	rejects := 0
	w := NewSingboxWriter(singboxFixture(), func(r access.Rollup) error { records = append(records, r); return nil }, func() { rejects++ })
	w.Write([]byte(nativeLine(1, "INFO", "inbound/socks[socks-in]", "inbound packet connection")))
	w.Write([]byte(nativeLine(1, "DEBUG", "router", "match[1] => route(direct)")))
	w.Write([]byte(nativeLine(1, "INFO", "outbound/direct[direct]", "outbound packet connection to 192.0.2.1:53")))
	if len(records) != 1 || records[0].UserID != "" || records[0].DeviceID != "" || records[0].Network != "udp" {
		t.Fatal(records)
	}
	for id := 2; id < 4100; id++ {
		w.Write([]byte(nativeLine(id, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to example.test:443")))
	}
	if len(w.pending) > 4096 || rejects == 0 {
		t.Fatal("unbounded native metadata correlation", len(w.pending), rejects)
	}
	w.now = func() time.Time { return time.Now().Add(3 * time.Minute) }
	w.Write([]byte(nativeLine(9999, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to other.test:443")))
	if len(w.pending) != 1 {
		t.Fatal("expired native metadata did not clear", len(w.pending))
	}
}

func TestSingboxMalformedMultilineDestinationQuarantinesInjectedMetadata(t *testing.T) {
	count, rejects := 0, 0
	w := NewSingboxWriter(singboxFixture(), func(access.Rollup) error { count++; return nil }, func() { rejects++ })
	w.Write([]byte(nativeLine(1, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to [malformed")))
	w.Write([]byte(nativeLine(42, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to forged.test:443")))
	w.Write([]byte(nativeLine(42, "DEBUG", "router", "match[1] => route(direct)")))
	if count != 0 || rejects != 1 || !w.poisoned {
		t.Fatal("multiline target created fabricated audit", count, rejects)
	}
}
