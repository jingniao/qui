package xray

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
)

func TestInboundEgressGroupUsesSourceHealth(t *testing.T) {
	const source = "192.0.2.30"
	first, second := "egress-primary", "egress-backup"
	healthy := scheduler.Health{Healthy: true, CheckedAt: goldenAt, LatencyMS: 1}
	slow := scheduler.Health{Healthy: true, CheckedAt: goldenAt, LatencyMS: 50}
	failed := scheduler.Health{CheckedAt: goldenAt}
	for _, tc := range []struct {
		name, strategy       string
		local, bound         map[string]scheduler.Health
		wantLocal, wantBound []string
	}{
		{"primary", "primary", map[string]scheduler.Health{first: healthy, second: healthy}, map[string]scheduler.Health{first: failed, second: healthy}, []string{first}, []string{second}},
		{"latency", "latency", map[string]scheduler.Health{first: healthy, second: slow}, map[string]scheduler.Health{first: slow, second: healthy}, []string{first}, []string{second}},
		{"round-robin", "round_robin", map[string]scheduler.Health{first: healthy, second: healthy}, map[string]scheduler.Health{first: failed, second: healthy}, []string{first, second}, []string{second}},
		{"only-source-alive", "round_robin", map[string]scheduler.Health{first: failed, second: failed}, map[string]scheduler.Health{first: healthy, second: healthy}, []string{"block"}, []string{first, second}},
		{"unknown-source", "primary", map[string]scheduler.Health{first: healthy}, nil, []string{first}, []string{"block"}},
		{"no-health", "primary", nil, nil, []string{"block"}, []string{"block"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := sourceScenarioBase()
			state.Network = &desiredstate.Network{OutboundInterface: "eth0"}
			group := &state.OutboundGroups[0]
			group.Strategy, group.Failover, group.BackupTargetID = tc.strategy, "Block", ""
			in := state.Inbounds[0]
			in.ID, in.Port, in.EgressSourceIP = "bound", 1081, source
			state.Inbounds = append(state.Inbounds, in)
			options, err := (&Adapter{}).options(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			options.Access, options.AccessLogPath = true, filepath.Join(t.TempDir(), accessLogName)
			for id, status := range tc.local {
				options.Health[id] = status
			}
			for id, status := range tc.bound {
				options.Health[core.ProbeKey(source, id)] = status
			}
			rendered, err := renderWithOptions(context.Background(), state, goldenAt, options)
			if err != nil {
				t.Fatal(err)
			}
			if binary := os.Getenv("XRAY_BINARY"); binary != "" {
				validator := Adapter{Path: binary, Directory: t.TempDir()}
				if err := validator.Validate(context.Background(), rendered); err != nil {
					t.Fatal(err)
				}
			}
			var doc configDocument
			if err := json.Unmarshal(rendered.Data, &doc); err != nil {
				t.Fatal(err)
			}
			outbounds := map[string]outboundConfig{}
			for _, out := range doc.Outbounds {
				outbounds[out.Tag] = out
			}
			for _, entry := range state.Inbounds {
				var matched *route
				for i, rule := range doc.Routing.Rules {
					if slices.Contains(rule.InboundTag, entry.ID) && slices.Contains(rule.User, "golden-user") {
						matched = &doc.Routing.Rules[i]
						break
					}
				}
				if matched == nil {
					t.Fatalf("missing %s account route", entry.ID)
				}
				tags := []string{matched.OutboundTag}
				if matched.BalancerTag != "" {
					tags = nil
					for _, b := range doc.Routing.Balancers {
						if b.Tag == matched.BalancerTag {
							tags = b.Selector
							if b.Strategy.Type != "roundRobin" {
								t.Errorf("unexpected strategy %s", b.Strategy.Type)
							}
							for _, selector := range tags {
								if doc.Observatory == nil || !slices.Contains(doc.Observatory.SubjectSelector, selector) {
									t.Errorf("missing observatory subject %q", selector)
								}
								for tag := range outbounds {
									if tag != selector && strings.HasPrefix(tag, selector) {
										t.Errorf("selector %q also matches %q", selector, tag)
									}
								}
							}
						}
					}
				}
				ids := []string{}
				for _, tag := range tags {
					exit := rendered.AccessContext.Exits[tag]
					ids = append(ids, exit.ID)
					if exit.AccountID != "device-credential" {
						t.Errorf("account attribution on %s: %+v", tag, exit)
					}
					out := outbounds[tag]
					if out.Protocol != "blackhole" && out.SendThrough != entry.EgressSourceIP {
						t.Errorf("selected %s sendThrough = %q, want %q", tag, out.SendThrough, entry.EgressSourceIP)
					}
				}
				want := tc.wantLocal
				if entry.EgressSourceIP != "" {
					want = tc.wantBound
				}
				if !slices.Equal(ids, want) {
					t.Errorf("%s picked %v, want %v", entry.ID, ids, want)
				}
			}
			probes := map[string]core.Probe{}
			for _, p := range rendered.Probes {
				probes[p.Key()] = p
			}
			for _, src := range []string{"", source} {
				for _, id := range []string{first, second} {
					p, ok := probes[core.ProbeKey(src, id)]
					if !ok {
						t.Errorf("missing probe from %q to %s", src, id)
						continue
					}
					var inbound string
					for _, in := range doc.Inbounds {
						if net.JoinHostPort(in.Listen, strconv.Itoa(in.Port)) == p.Address {
							inbound = in.Tag
						}
					}
					var out outboundConfig
					for _, rule := range doc.Routing.Rules {
						if slices.Contains(rule.InboundTag, inbound) {
							out = outbounds[rule.OutboundTag]
							break
						}
					}
					if out.SendThrough != src || rendered.AccessContext.Exits[out.Tag].ID != id {
						t.Errorf("probe from %q to %s reached %q (%s)", src, id, out.Tag, out.SendThrough)
					}
					if src != "" && out.Stream != nil && out.Stream.Sockopt != nil && out.Stream.Sockopt.Interface != "" {
						t.Errorf("source probe %s inherited node interface", out.Tag)
					}
				}
			}
		})
	}
}

func TestInboundEgressProbeCacheIdentity(t *testing.T) {
	const source = "192.0.2.30"
	state := sourceScenarioBase()
	state.Inbounds[0].EgressSourceIP = source
	a := &Adapter{}
	ctx := context.Background()
	options, err := a.options(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	config, err := renderWithOptions(ctx, state, goldenAt, options)
	if err != nil {
		t.Fatal(err)
	}
	a.current = config
	a.outboundHealth = map[string]scheduler.Health{}
	raw := "egress-backup"
	bound := core.ProbeKey(source, raw)
	a.outboundHealth[raw] = scheduler.Health{CheckedAt: goldenAt}
	a.outboundHealth[bound] = scheduler.Health{Healthy: true, CheckedAt: goldenAt}
	healthy := a.OutboundHealth()
	if len(healthy) != 1 || healthy[raw].Healthy {
		t.Fatal("panel health mixed source paths", healthy)
	}
	cached, err := a.options(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Health) != 2 || !cached.Health[bound].Healthy {
		t.Fatal("source health was not reused", cached.Health)
	}
	state.Inbounds[0].EgressSourceIP = "192.0.2.31"
	changed, err := a.options(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	next := core.ProbeKey("192.0.2.31", raw)
	if len(changed.Health) != 1 || changed.ProbePorts[next] == options.ProbePorts[bound] {
		t.Fatal("new source reused a previous health observation or probe endpoint")
	}
	state.Inbounds[0].EgressSourceIP = source
	state.Credentials[1].Secret = "rotated-relay-password"
	rotated, err := a.options(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated.Health) != 0 {
		t.Fatal("rotated relay credentials retained health", rotated.Health)
	}
}

func TestInboundEgressResolvesPolicyAndDefaultFailoverSeparately(t *testing.T) {
	const source = "192.0.2.30"
	state := sourceScenarioBase()
	state.Inbounds[0].EgressSourceIP = source
	primary := &state.OutboundGroups[0]
	backup := *primary
	backup.ID, backup.Failover, backup.BackupTargetID = "backup-pool", "Block", ""
	backup.Members = primary.Members[1:]
	primary.Members = primary.Members[:1]
	primary.Failover, primary.BackupTargetID = "BackupOutboundGroup", backup.ID
	state.OutboundGroups = append(state.OutboundGroups, backup)
	state.Policies = []desiredstate.Policy{{
		ID: "service", Enabled: true, Match: desiredstate.Match{Domains: []string{"service.example"}},
		Action: desiredstate.Action{Type: "OutboundGroup", TargetID: "egress-pool", Failover: "Direct"},
	}}
	healthy := scheduler.Health{Healthy: true, CheckedAt: goldenAt}
	options := RenderOptions{
		Access: true, AccessLogPath: filepath.Join(t.TempDir(), accessLogName),
		Health: map[string]scheduler.Health{"egress-primary": healthy, core.ProbeKey(source, "egress-backup"): healthy},
	}
	config, err := renderWithOptions(context.Background(), state, goldenAt, options)
	if err != nil {
		t.Fatal(err)
	}
	var doc configDocument
	if err := json.Unmarshal(config.Data, &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, rule := range doc.Routing.Rules {
		if !slices.Contains(rule.InboundTag, "entry") || !slices.Contains(rule.User, "golden-user") {
			continue
		}
		exit := config.AccessContext.Exits[rule.OutboundTag]
		if _, seen := got[exit.RuleID]; !seen {
			got[exit.RuleID] = exit.ID
			if exit.AccountID != "device-credential" {
				t.Errorf("missing credential attribution: %+v", exit)
			}
		}
	}
	if got[""] != "egress-backup" || got["service"] != "direct" {
		t.Fatalf("bound default and policy resolved to %v", got)
	}
}
