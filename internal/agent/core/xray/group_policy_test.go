package xray

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealGroupPolicySelectionDoesNotInheritDirectFailover(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for real policy group routing")
	}
	ctx := context.Background()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-origin") }))
	defer origin.Close()
	first := httpProxyServer(t, "first-egress", 0)
	second := httpProxyServer(t, "second-egress", 0)
	state := testState(availablePort(t), 1)
	for index, server := range []*httptest.Server{first, second} {
		parsed, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(parsed.Port())
		state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: []string{"first", "second"}[index], Protocol: "http", Address: "127.0.0.1", Port: port, Enabled: true})
	}
	state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "pool", Strategy: "round_robin", ProbeURL: origin.URL, ProbeInterval: 3, Failover: "Direct", Members: []desiredstate.GroupMember{{OutboundID: "first", Weight: 1}, {OutboundID: "second", Weight: 1}}}}
	state.Policies = []desiredstate.Policy{{ID: "service-pool", Enabled: true, Match: desiredstate.Match{CIDRs: []string{"127.0.0.1/32"}, Network: "tcp"}, Action: desiredstate.Action{Type: "OutboundGroup", TargetID: "pool", Failover: "Block"}}}
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID}
	defer adapter.Close()
	manager, err := reconcile.New(t.TempDir(), state.NodeID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if _, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL); allowed {
		t.Fatal("policy inherited Direct with unknown health")
	}
	if err := adapter.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for index := 0; index < 4; index++ {
		body, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL)
		if !allowed {
			t.Fatal("healthy policy group blocked")
		}
		if strings.Contains(body, "first-egress") {
			counts["first"]++
		} else if strings.Contains(body, "second-egress") {
			counts["second"]++
		} else {
			t.Fatal("policy bypassed pool", body)
		}
	}
	if counts["first"] != 2 || counts["second"] != 2 {
		t.Fatal("policy group not round robin", counts)
	}
	first.Close()
	second.Close()
	time.Sleep(3050 * time.Millisecond)
	adapter.Probe(ctx)
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if _, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL); allowed {
		t.Fatal("policy inherited Direct after member failures")
	}
}
