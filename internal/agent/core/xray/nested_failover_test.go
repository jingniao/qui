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

func TestRealNestedFallbackGroupChainAndTerminalBlock(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for nested fallback traffic")
	}
	ctx := context.Background()
	primary := httpProxyServer(t, "primary", 0)
	secondary := httpProxyServer(t, "secondary", 0)
	last := httpProxyServer(t, "last-chain", 0)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-origin") }))
	defer origin.Close()
	state := testState(availablePort(t), 1)
	for index, server := range []*httptest.Server{primary, secondary, last} {
		parsed, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(parsed.Port())
		state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: []string{"primary", "secondary", "last"}[index], Protocol: "http", Address: "127.0.0.1", Port: port, Enabled: true, ProbeURL: origin.URL, ProbeInterval: 3})
	}
	state.Outbounds[2].FailurePolicy = "BackupOutboundGroup"
	state.Outbounds[2].BackupTargetID = "pool"
	state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "pool", Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: "secondary", Weight: 1}}, ProbeURL: origin.URL, ProbeInterval: 3, Failover: "BackupProxyChain", BackupTargetID: "backup-chain"}}
	state.ProxyChains = []desiredstate.ProxyChain{{ID: "backup-chain", NodeIDs: []string{state.NodeID, "exit"}, OutboundIDs: []string{"last"}, Failover: "Block"}}
	// The last entry represents a real protocol connection; the complete multi
	// node path is covered by the three-Agent suite. This test isolates recursive
	// selection and terminal failure behavior with identifiable egress servers.
	state.Inbounds[0].DefaultOutbound = "primary"
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID}
	defer adapter.Close()
	manager, err := reconcile.New(t.TempDir(), state.NodeID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := adapter.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		body, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL)
		if !allowed || !strings.Contains(body, want) {
			t.Fatal("unexpected egress", want, body)
		}
	}
	check("primary")
	// Primary -> group -> chain, then the last hop's own TCP failure blocks.
	primary.Close()
	time.Sleep(3050 * time.Millisecond)
	adapter.Probe(ctx)
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check("secondary")
	secondary.Close()
	time.Sleep(3050 * time.Millisecond)
	adapter.Probe(ctx)
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check("last-chain")
	last.Close()
	if _, allowed := groupRequest(t, state.Inbounds[0].Port, origin.URL); allowed {
		t.Fatal("terminal failure fell through to Direct")
	}
}

func TestFallbackRenderingIsDeterministicAndPolicyBlockOverridesOutbound(t *testing.T) {
	state := testState(1080, 1)
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: "primary", Protocol: "http", Address: "127.0.0.1", Port: 8080, Enabled: true, FailurePolicy: "Direct"})
	state.Policies = []desiredstate.Policy{{ID: "fixed-exit", Enabled: true, Match: desiredstate.Match{Domains: []string{"fixed.example"}, Network: "tcp"}, Action: desiredstate.Action{Type: "Outbound", TargetID: "primary", Failover: "Block"}}}
	var first []byte
	for index := 0; index < 20; index++ {
		rendered, err := Render(context.Background(), state)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = rendered.Data
		} else if string(first) != string(rendered.Data) {
			t.Fatal("identical state changed rendering order")
		}
		if !strings.Contains(string(rendered.Data), `"outboundTag": "_policy_raw_fixed-exit"`) {
			t.Fatal("Block rule inherited standalone Direct fallback")
		}
	}
}
