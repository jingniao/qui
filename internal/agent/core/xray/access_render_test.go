package xray

import (
	"context"
	"encoding/json"
	"path/filepath"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"strings"
	"testing"
	"time"
)

func TestAccessInstrumentationKeepsTwoInboundDefaultGroupsSeparate(t *testing.T) {
	state := testState(1080, 1)
	state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "device-user", Secret: "PRIVATE-SECRET"}}
	state.Inbounds[0].CredentialIDs = []string{"credential"}
	state.Inbounds[0].DefaultOutbound = "group-one"
	second := state.Inbounds[0]
	second.ID = "second-inbound"
	second.Port = 1081
	second.DefaultOutbound = "group-two"
	state.Inbounds = append(state.Inbounds, second)
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: "one", Enabled: true, Protocol: "http", Address: "192.0.2.1", Port: 8080}, desiredstate.Outbound{ID: "two", Enabled: true, Protocol: "http", Address: "192.0.2.2", Port: 8080})
	for _, id := range []string{"one", "two"} {
		state.OutboundGroups = append(state.OutboundGroups, desiredstate.OutboundGroup{ID: "group-" + id, Strategy: "round_robin", Members: []desiredstate.GroupMember{{OutboundID: id, Weight: 1}}, ProbeURL: "https://probe.example", ProbeInterval: 30, Failover: "Block"})
	}
	options := RenderOptions{Access: true, AccessLogPath: filepath.Join(t.TempDir(), accessLogName), Health: map[string]scheduler.Health{"one": {Healthy: true, CheckedAt: time.Now()}, "two": {Healthy: true, CheckedAt: time.Now()}}}
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), options)
	if err != nil {
		t.Fatal(err)
	}
	var document configDocument
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	for _, rule := range document.Routing.Rules {
		if len(rule.User) == 0 {
			continue
		}
		want := "one"
		if rule.InboundTag[0] == "second-inbound" {
			want = "two"
		}
		found := false
		for _, balancer := range document.Routing.Balancers {
			if balancer.Tag == rule.BalancerTag {
				for _, tag := range balancer.Selector {
					exit := rendered.AccessContext.Exits[tag]
					if exit.ID != want || exit.AccountID != "credential" {
						t.Fatal("default groups mixed by telemetry aliases", want, exit)
					}
					found = true
				}
			}
		}
		if !found {
			t.Fatal("account default balancer missing")
		}
	}
	encoded, _ := json.Marshal(rendered.AccessContext)
	if strings.Contains(string(encoded), "PRIVATE-SECRET") {
		t.Fatal("credential in access context")
	}
	again, err := renderWithOptions(context.Background(), state, time.Now(), options)
	if err != nil || string(again.Data) != string(rendered.Data) {
		t.Fatal("instrumented rendering not deterministic", err)
	}
}

func TestAccessLoggingDisabledPreservesBlockAndDoesNotCollect(t *testing.T) {
	state := testState(1080, 1)
	state.Policies = []desiredstate.Policy{{ID: "private", Enabled: true, Match: desiredstate.Match{Domains: []string{"private.example"}}, Action: desiredstate.Action{Type: "Block"}, Logging: &desiredstate.PolicyLogging{LogDomain: false}}}
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), RenderOptions{Access: true, AccessLogPath: filepath.Join(t.TempDir(), accessLogName)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, exit := range rendered.AccessContext.Exits {
		if exit.RuleID == "private" {
			found = true
			if exit.LogDomain || exit.Protocol != "block" {
				t.Fatal("logging setting changed policy behavior", exit)
			}
		}
	}
	if !found {
		t.Fatal("actual rule telemetry metadata missing")
	}
}
