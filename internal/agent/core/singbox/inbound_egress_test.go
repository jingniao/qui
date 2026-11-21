package singbox

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

func validateEgressNative(t *testing.T, config core.RenderedConfig) {
	t.Helper()
	if binary := os.Getenv("SINGBOX_BINARY"); binary != "" {
		native := &Adapter{Path: binary, Directory: t.TempDir()}
		if err := native.Validate(context.Background(), config); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInboundEgressIndependentSelection(t *testing.T) {
	for _, strategy := range []string{"primary", "latency", "round_robin"} {
		t.Run(strategy, func(t *testing.T) {
			state := sourceScenarioBase()
			state.OutboundGroups[0].Strategy = strategy
			state.Inbounds[0].EgressSourceIP = "192.0.2.10"
			second := state.Inbounds[0]
			second.ID, second.Port, second.EgressSourceIP = "unobserved", 1081, "192.0.2.20"
			state.Inbounds = append(state.Inbounds, second)
			state.Policies = []desiredstate.Policy{{ID: "rule", Enabled: true, Match: desiredstate.Match{Domains: []string{"example.com"}}, Action: desiredstate.Action{Type: "OutboundGroup", TargetID: "egress-pool", Failover: "Block"}}}
			ok := scheduler.Health{Healthy: true, CheckedAt: goldenAt, LatencyMS: 10}
			down := scheduler.Health{CheckedAt: goldenAt}
			health := map[string]scheduler.Health{
				"egress-primary": ok, "egress-backup": down,
				"192.0.2.10|egress-primary": down, "192.0.2.10|egress-backup": ok,
			}
			if strategy == "latency" {
				slow := scheduler.Health{Healthy: true, CheckedAt: goldenAt, LatencyMS: 500}
				health["egress-backup"], health["192.0.2.10|egress-primary"] = slow, slow
			}
			rendered, err := renderWithOptions(context.Background(), state, goldenAt, renderOptions{access: true, health: health})
			if err != nil {
				t.Fatal(err)
			}
			var doc document
			if err := json.Unmarshal(rendered.Data, &doc); err != nil {
				t.Fatal(err)
			}
			firstPolicies := map[string]bool{}
			for i, rule := range doc.Route.Rules {
				if len(rule.Domain) == 0 || len(rule.Inbound) == 0 || firstPolicies[rule.Inbound[0]] {
					continue
				}
				firstPolicies[rule.Inbound[0]] = true
				if rule.Inbound[0] == "unobserved" {
					if rule.Action != "reject" {
						t.Errorf("unknown source should Block, got %+v", rule)
					}
					continue
				}
				want := "egress-backup~egress~0"
				tag := collector.SingboxRuleTag(i)
				exit := rendered.AccessContext.Exits[tag]
				if strategy == "primary" {
					if rule.Outbound != want {
						t.Errorf("primary chose %q, want %q", rule.Outbound, want)
					}
				} else {
					group := slices.IndexFunc(doc.Outbounds, func(out outbound) bool { return out.Tag == rule.Outbound })
					if group < 0 || !slices.Equal(doc.Outbounds[group].Outbounds, []string{want}) {
						t.Errorf("urltest %q lacks the right source members", rule.Outbound)
					}
					exit = rendered.AccessContext.Selections[tag][want]
				}
				if exit.ID != "egress-backup" || exit.NativeTag != want || exit.RuleID != "rule" {
					t.Errorf("wrong selected member attribution: %+v", exit)
				}
			}
			if !firstPolicies["entry"] || !firstPolicies["unobserved"] {
				t.Fatal("missing source-scoped policies")
			}
			validateEgressNative(t, rendered)
		})
	}
}

func TestInboundEgressProbesCoverFailedRawMembers(t *testing.T) {
	state := sourceScenarioBase()
	state.Inbounds[0].EgressSourceIP = "192.0.2.10"
	a := &Adapter{statsChecked: true, Now: func() time.Time { return goldenAt }}
	rendered, err := a.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.Probes) != 4 {
		t.Fatalf("unknown health yielded %d probes, want all four default and source paths", len(rendered.Probes))
	}
	var doc document
	if err := json.Unmarshal(rendered.Data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range rendered.Probes {
		_, port, err := net.SplitHostPort(p.Address)
		if err != nil {
			t.Fatal(err)
		}
		in := slices.IndexFunc(doc.Inbounds, func(in inbound) bool { return strconv.Itoa(in.Port) == port })
		if in < 0 {
			t.Fatalf("probe %q has no listener", p.Key())
		}
		target := p.OutboundID
		if p.SourceIP != "" {
			target += "~egress~0"
		}
		out := slices.IndexFunc(doc.Outbounds, func(out outbound) bool { return out.Tag == target })
		if out < 0 || doc.Outbounds[out].Inet4BindAddress != p.SourceIP {
			t.Fatalf("missing raw probe outbound %q with source %q", target, p.SourceIP)
		}
		r := slices.IndexFunc(doc.Route.Rules, func(r route) bool { return r.Outbound == target && slices.Contains(r.Inbound, doc.Inbounds[in].Tag) })
		if r < 0 {
			t.Fatalf("probe %q does not route to raw target %q", p.Key(), target)
		}
	}
	a.current = rendered
	a.outboundHealth = map[string]scheduler.Health{"192.0.2.10|egress-primary": {Healthy: true, CheckedAt: goldenAt}}
	if len(a.OutboundHealth()) != 0 {
		t.Fatal("source observations leaked into panel health")
	}
	validateEgressNative(t, rendered)
}

func TestInboundEgressBackupChainPreservesWireGuardAndAccount(t *testing.T) {
	scenario := sourceScenarios()[4]
	state, options := scenario.state, scenario.options
	const source = "192.0.2.55"
	state.Inbounds[0].EgressSourceIP = source
	state.ProxyChains = []desiredstate.ProxyChain{{ID: "fallback-chain", NodeIDs: []string{state.NodeID, "exit-node"}, OutboundIDs: []string{"wg-out"}, Failover: "Block"}}
	state.Policies = []desiredstate.Policy{{ID: "via-pool", Enabled: true, Match: desiredstate.Match{Domains: []string{"example.com"}}, Action: desiredstate.Action{Type: "OutboundGroup", TargetID: "egress-pool", Failover: "BackupProxyChain", BackupID: "fallback-chain"}}}
	options.access = true
	rendered, err := renderWithOptions(context.Background(), state, goldenAt, options)
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal(rendered.Data, &doc); err != nil {
		t.Fatal(err)
	}
	const target = "wg-out~egress~0"
	endpoint := slices.IndexFunc(doc.Endpoints, func(ep wireGuardEndpoint) bool { return ep.Tag == target })
	if endpoint < 0 || doc.Endpoints[endpoint].Inet4BindAddress != source || doc.Endpoints[endpoint].BindInterface != "" {
		t.Fatal("fallback WireGuard endpoint did not bind the inbound source")
	}
	rule := slices.IndexFunc(doc.Route.Rules, func(r route) bool { return r.Outbound == target && slices.Contains(r.Inbound, "entry") })
	if rule < 0 {
		t.Fatal("missing source-specific fallback chain route")
	}
	record, logged, err := rendered.AccessContext.Record(collector.XrayEvent{InboundID: "entry", OutboundTag: collector.SingboxRuleTag(rule), AccountID: "golden-user", Domain: "example.com", Port: 443, Network: "tcp"}, goldenAt)
	if err != nil || !logged || record.OutboundID != "wg-out" || record.ChainID != "fallback-chain" || record.RuleID != "via-pool" || record.UserID != "user" || record.DeviceID != "device" {
		t.Fatalf("fallback access attribution: %+v, logged=%v, err=%v", record, logged, err)
	}
	validateEgressNative(t, rendered)
}

func TestInboundEgressProbeFingerprintIsolatesEffectiveNetwork(t *testing.T) {
	state := sourceScenarioBase()
	state.Inbounds[0].EgressSourceIP = "192.0.2.10"
	a := &Adapter{statsChecked: true, Now: func() time.Time { return goldenAt }}
	first, err := a.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	state.Network = &desiredstate.Network{OutboundInterface: "eth2"}
	changed, err := a.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	raw, bound := "egress-primary", "192.0.2.10|egress-primary"
	if first.ProbeFingerprints[bound] == "" || first.ProbeFingerprints[raw] == first.ProbeFingerprints[bound] ||
		first.ProbeFingerprints[raw] == changed.ProbeFingerprints[raw] || first.ProbeFingerprints[bound] != changed.ProbeFingerprints[bound] {
		t.Fatal("fingerprints failed to distinguish the effective dial paths")
	}
	state.Inbounds[0].EgressSourceIP = "192.0.2.11"
	next, err := a.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if next.ProbeFingerprints[core.ProbeKey("192.0.2.11", raw)] == changed.ProbeFingerprints[bound] {
		t.Fatal("changed source IP reused a probe fingerprint")
	}
}

func TestRealSingboxInboundEgressProbesDriveSelection(t *testing.T) {
	binary := schedulerBinary(t)
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() != nil && ip.IsGlobalUnicast() {
			source = ip.String()
			break
		}
	}
	if source == "" {
		t.Skip("requires a local non-loopback IPv4 address")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if r.URL.Path == "/probe" && (err != nil || !net.ParseIP(host).IsLoopback()) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "direct")
	}))
	defer origin.Close()
	state := stateFixture(t, "http", freePort(t))
	state.OutboundGroups = []desiredstate.OutboundGroup{{
		ID: "pool", Strategy: "primary", Failover: "Block", ProbeURL: origin.URL + "/probe", ProbeInterval: 30,
		Members: []desiredstate.GroupMember{{OutboundID: "direct", Weight: 1}},
	}}
	state.Inbounds[0].DefaultOutbound = "group:pool"
	bound := state.Inbounds[0]
	bound.ID, bound.Port, bound.EgressSourceIP = "bound", freePort(t), source
	state.Inbounds = append(state.Inbounds, bound)
	a := &Adapter{Path: binary, NodeID: state.NodeID, Directory: t.TempDir()}
	defer a.Close()
	manager, err := reconcile.New(t.TempDir(), state.NodeID, a)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	probeErr := a.Probe(ctx)
	raw, pinned := a.outboundHealth["direct"], a.outboundHealth[core.ProbeKey(source, "direct")]
	if !raw.Healthy || pinned.CheckedAt.IsZero() || pinned.Healthy {
		t.Fatalf("default probe %+v, bound probe %+v, probe error %v", raw, pinned, probeErr)
	}
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatalf("probe results did not change the running routes: changed=%t, err=%v", changed, err)
	}
	var doc document
	if err := json.Unmarshal(a.current.Data, &doc); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(doc.Route.Rules, func(r route) bool { return slices.Contains(r.Inbound, bound.ID) })
	if i < 0 || doc.Route.Rules[i].Action != "reject" {
		t.Fatal("failed source did not apply its Block policy")
	}
	if body, err := groupBody(state.Inbounds[0].Port, origin.URL); err != nil || body != "direct" {
		t.Fatal("healthy default source failed to serve direct traffic", body, err)
	}
	if body, err := groupBody(bound.Port, origin.URL); err == nil {
		t.Fatal("failed source served traffic", body)
	}
}
