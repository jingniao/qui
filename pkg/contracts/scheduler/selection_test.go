package scheduler

import (
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func TestPrimaryLatencyAndStaleHealth(t *testing.T) {
	now := time.Now().UTC()
	group := desiredstate.OutboundGroup{ID: "group", Strategy: "primary", ProbeURL: "https://probe.example.test/health", ProbeInterval: 30, Failover: "Block", Members: []desiredstate.GroupMember{{OutboundID: "a", Priority: 1, Weight: 2}, {OutboundID: "b", Priority: 2, Weight: 1}}}
	if err := ValidateGroup(group, map[string]bool{"a": true, "b": true}); err != nil {
		t.Fatal(err)
	}
	health := map[string]Health{"a": {Healthy: true, LatencyMS: 80, CheckedAt: now}, "b": {Healthy: true, LatencyMS: 10, CheckedAt: now}}
	members := HealthyMembers(group, health, now)
	if len(members) != 1 || members[0].OutboundID != "a" {
		t.Fatal("primary preference ignored")
	}
	group.Strategy = "latency"
	members = HealthyMembers(group, health, now)
	if len(members) != 1 || members[0].OutboundID != "b" {
		t.Fatal("latency preference ignored")
	}
	group.Strategy = "round_robin"
	if members := HealthyMembers(group, health, now); len(members) != 2 {
		t.Fatal("round robin must keep every healthy member", members)
	}
	// The removed weighted strategy is no longer accepted (plan D4).
	group.Strategy = "weighted"
	if err := ValidateGroup(group, map[string]bool{"a": true, "b": true}); err == nil {
		t.Fatal("removed weighted strategy accepted")
	}
	health["a"] = Health{Healthy: true, CheckedAt: now.Add(-time.Hour)}
	health["b"] = Health{Healthy: false, CheckedAt: now}
	if members := HealthyMembers(group, health, now); len(members) != 0 {
		t.Fatal("stale or unhealthy selected")
	}
	if err := ValidateProbeURL("https://user:password@probe.test/path?token=value"); err == nil {
		t.Fatal("credential-bearing probe accepted")
	}
}
