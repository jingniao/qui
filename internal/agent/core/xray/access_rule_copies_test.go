package xray

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// A policy's match list (with native rule-set references Xray loads once per
// reference) must not be copied per inbound×credential on protocols whose
// access line already carries the account; shlii's 256 MB container ran out
// of memory validating such copies. WireGuard keeps per-credential copies
// because its per-peer counters read the cloned outbounds.
func TestAccessPolicyRuleSetsAreNotCopiedPerEmailCredential(t *testing.T) {
	domain, address := testRuleSets()
	state := testState(1080, 1)
	state.Users = []desiredstate.User{{ID: "a", Enabled: true}, {ID: "b", Enabled: true}}
	state.Credentials = []desiredstate.Credential{
		{ID: "vless-a", UserID: "a", DeviceID: "da", Protocol: "vless", Secret: "11111111-1111-4111-8111-111111111111"},
		{ID: "vless-b", UserID: "b", DeviceID: "db", Protocol: "vless", Secret: "22222222-2222-4222-8222-222222222222"},
		{ID: "ss-a", UserID: "a", DeviceID: "da", Protocol: "shadowsocks", Secret: "c2hhZG93c29ja3Mtc2VjcmV0LXRlc3QtdmFsdWUtMzI="},
	}
	state.Inbounds = []desiredstate.Inbound{
		{ID: "vless-in", Protocol: "vless", Listen: "0.0.0.0", Port: 2001, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"vless-a", "vless-b"}},
		{ID: "ss-in", Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 2002, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"ss-a"}},
		{ID: "ss-in-2", Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 2003, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"ss-a"}},
	}
	state.Policies = []desiredstate.Policy{ruleSetPolicy(domain, address)}
	options := RenderOptions{Access: true, AccessLogPath: filepath.Join(t.TempDir(), accessLogName)}
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), options)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(rendered.Data), `"ext:`); got != 2 {
		t.Fatalf("rule-set references = %d, want one domain and one IP rule", got)
	}
	var document configDocument
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	for _, rule := range document.Routing.Rules {
		if rule.RuleTag != "block-cn" {
			continue
		}
		if len(rule.User) != 0 || len(rule.InboundTag) != 0 {
			t.Fatalf("policy rule must stay unscoped: %+v", rule)
		}
		exit := rendered.AccessContext.Exits[rule.OutboundTag]
		if exit.RuleID != "block-cn" || exit.AccountID != "" {
			t.Fatalf("policy attribution: %+v", exit)
		}
	}
	// The collector still resolves the account from the access line's email.
	for _, id := range []string{"vless-a", "vless-b"} {
		if _, ok := rendered.AccessContext.Inbounds["vless-in"].Accounts[id]; !ok {
			t.Fatalf("access context lost account %s", id)
		}
	}

	// WireGuard (per-peer counters) and HTTP/SOCKS5 (usernames) keep their copies.
	if !accessLogsAccount("vless") || !accessLogsAccount("shadowsocks") || !accessLogsAccount("trojan") {
		t.Fatal("email protocols must use the access line")
	}
	for _, protocol := range []string{"wireguard", "http", "socks5"} {
		if accessLogsAccount(protocol) {
			t.Fatalf("%s must keep per-credential copies", protocol)
		}
	}
}
