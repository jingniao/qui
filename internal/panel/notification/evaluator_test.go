package notification

import (
	"testing"
	"time"
)

func floatPtr(v float64) *float64 { return &v }
func uint64Ptr(v uint64) *uint64  { return &v }

func defaultRules() []Rule {
	return []Rule{
		{Kind: "node_offline", Enabled: true, Threshold: 0, DurationMinutes: 3, CooldownMinutes: 30},
		{Kind: "cpu", Enabled: true, Threshold: 90, DurationMinutes: 5, CooldownMinutes: 30},
		{Kind: "memory", Enabled: true, Threshold: 90, DurationMinutes: 5, CooldownMinutes: 30},
		{Kind: "disk", Enabled: true, Threshold: 90, DurationMinutes: 0, CooldownMinutes: 30},
		{Kind: "public_ip", Enabled: true, Threshold: 0, DurationMinutes: 0, CooldownMinutes: 30},
		{Kind: "machine_traffic", Enabled: true, Threshold: 100, DurationMinutes: 0, CooldownMinutes: 30},
		{Kind: "system_alert", Enabled: true, Threshold: 0, DurationMinutes: 0, CooldownMinutes: 30},
	}
}

func TestOfflineFiresAfterDurationAndResolves(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// 1. Offline but only 2 minutes (less than 3 min duration) -> no event
	snap1 := Snapshot{
		Now: now,
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-2 * time.Minute)},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// 2. Offline for 3.5 minutes -> fires critical offline event
	snap2 := Snapshot{
		Now: now.Add(90 * time.Second),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-2 * time.Minute)},
		},
	}
	events, states = Evaluate(rules, snap2, states)
	if len(events) != 1 || events[0].Kind != "node_offline" || events[0].Severity != "critical" || events[0].Resolved {
		t.Fatalf("expected 1 critical offline event, got %+v", events)
	}

	// 3. Node comes back online -> resolves
	snap3 := Snapshot{
		Now: now.Add(2 * time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "online", LastHeartbeat: now.Add(2 * time.Minute)},
		},
	}
	events, states = Evaluate(rules, snap3, states)
	if len(events) != 1 || events[0].Kind != "node_offline" || !events[0].Resolved || events[0].Severity != "resolved" {
		t.Fatalf("expected resolved offline event, got %+v", events)
	}
	if states["node_offline:node1"].Active {
		t.Fatal("expected state to be inactive after resolve")
	}
}

func TestPendingNodeIsNotOffline(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// Pending node has zero or old heartbeat, but must not trigger offline alert
	snap := Snapshot{
		Now: now,
		Nodes: []NodeSnapshot{
			{ID: "node_pending", Name: "PendingNode", Status: "pending", LastHeartbeat: time.Time{}},
			{ID: "node_disabled", Name: "DisabledNode", Status: "disabled", LastHeartbeat: now.Add(-10 * time.Minute)},
		},
	}
	events, _ := Evaluate(rules, snap, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for pending and disabled nodes, got %d", len(events))
	}
}

func TestCPUNeedsFullWindow(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// CPU duration is 5 minutes. 4 high samples + 1 low sample should NOT trigger
	snap1 := Snapshot{
		Now:   now,
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online"}},
		Metrics: map[string]MetricWindow{
			"node1": {
				NodeID: "node1",
				CPU:    []*float64{floatPtr(95), floatPtr(95), floatPtr(95), floatPtr(85), floatPtr(95)},
			},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events when window has a low sample, got %+v", events)
	}

	// 5 high samples (>= 90%) -> triggers warning
	snap2 := Snapshot{
		Now:   now.Add(time.Minute),
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online"}},
		Metrics: map[string]MetricWindow{
			"node1": {
				NodeID: "node1",
				CPU:    []*float64{floatPtr(95), floatPtr(92), floatPtr(91), floatPtr(94), floatPtr(96)},
			},
		},
	}
	events, states = Evaluate(rules, snap2, states)
	if len(events) != 1 || events[0].Kind != "cpu" || events[0].Severity != "warning" {
		t.Fatalf("expected 1 cpu warning event, got %+v", events)
	}

	// Later, latest sample drops below 90% -> resolves
	snap3 := Snapshot{
		Now:   now.Add(2 * time.Minute),
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online"}},
		Metrics: map[string]MetricWindow{
			"node1": {
				NodeID: "node1",
				CPU:    []*float64{floatPtr(92), floatPtr(91), floatPtr(94), floatPtr(96), floatPtr(88)},
			},
		},
	}
	events, states = Evaluate(rules, snap3, states)
	if len(events) != 1 || events[0].Kind != "cpu" || !events[0].Resolved {
		t.Fatalf("expected 1 cpu resolved event, got %+v", events)
	}
}

func TestMissingSamplesNeitherFireNorResolve(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// All nil samples
	snap1 := Snapshot{
		Now:   now,
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online"}},
		Metrics: map[string]MetricWindow{
			"node1": {
				NodeID: "node1",
				CPU:    []*float64{nil, nil, nil, nil, nil},
			},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for all nil samples, got %+v", events)
	}

	// Active CPU state already exists; now samples are all nil: should NOT resolve
	activeStates := map[string]State{
		"cpu:node1": {Key: "cpu:node1", Active: true, Since: now, LastNotified: now},
	}
	events, states = Evaluate(rules, snap1, activeStates)
	if len(events) != 0 {
		t.Fatalf("expected 0 events when all samples are nil (neither fire nor resolve), got %+v", events)
	}
	if !states["cpu:node1"].Active {
		t.Fatal("expected state to remain active when samples are missing")
	}
}

func TestCooldownSuppressesRepeat(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// 1. First trigger
	snap1 := Snapshot{
		Now: now,
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-5 * time.Minute)},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// 2. 10 minutes later (still offline, but cooldown is 30m) -> suppressed
	snap2 := Snapshot{
		Now: now.Add(10 * time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-15 * time.Minute)},
		},
	}
	events, states = Evaluate(rules, snap2, states)
	if len(events) != 0 {
		t.Fatalf("expected cooldown to suppress repeat alert, got %+v", events)
	}

	// 3. 31 minutes later -> cooldown expired, repeats alert
	snap3 := Snapshot{
		Now: now.Add(31 * time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-36 * time.Minute)},
		},
	}
	events, states = Evaluate(rules, snap3, states)
	if len(events) != 1 || events[0].Resolved {
		t.Fatalf("expected repeated alert after cooldown expiry, got %+v", events)
	}
}

func TestPublicIPChangeFirstSightIsSilent(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// 1. First sight: records state silently
	snap1 := Snapshot{
		Now: now,
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Alpine", Status: "online", Addresses: []string{"203.0.113.5"}},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 0 {
		t.Fatalf("expected first sight to be silent, got %+v", events)
	}
	if states["public_ip:node1"].Value != "203.0.113.5" {
		t.Fatalf("expected recorded IP, got %+v", states["public_ip:node1"])
	}

	// 2. Address changed -> fires info event
	snap2 := Snapshot{
		Now: now.Add(time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Alpine", Status: "online", Addresses: []string{"203.0.113.9"}},
		},
	}
	events, states = Evaluate(rules, snap2, states)
	if len(events) != 1 || events[0].Kind != "public_ip" || events[0].Severity != "info" {
		t.Fatalf("expected 1 public_ip info event, got %+v", events)
	}
	if states["public_ip:node1"].Value != "203.0.113.9" {
		t.Fatalf("expected updated IP in state, got %+v", states["public_ip:node1"])
	}
}

func TestMachineTrafficThreshold(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// 100GB used of 100GB allowance -> 100% >= threshold 100% -> fires warning
	snap1 := Snapshot{
		Now:   now,
		Nodes: []NodeSnapshot{{ID: "node1", Name: "HK", Status: "online"}},
		Traffic: map[string]TrafficSnapshot{
			"node1": {NodeID: "node1", UsedBytes: uint64Ptr(100), AllowanceBytes: uint64Ptr(100)},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 1 || events[0].Kind != "machine_traffic" || events[0].Severity != "warning" {
		t.Fatalf("expected 1 machine_traffic warning, got %+v", events)
	}

	// New billing cycle: used drops to 10GB -> resolves
	snap2 := Snapshot{
		Now:   now.Add(time.Hour),
		Nodes: []NodeSnapshot{{ID: "node1", Name: "HK", Status: "online"}},
		Traffic: map[string]TrafficSnapshot{
			"node1": {NodeID: "node1", UsedBytes: uint64Ptr(10), AllowanceBytes: uint64Ptr(100)},
		},
	}
	events, states = Evaluate(rules, snap2, states)
	if len(events) != 1 || events[0].Kind != "machine_traffic" || !events[0].Resolved {
		t.Fatalf("expected 1 machine_traffic resolved event, got %+v", events)
	}
}

func TestEvaluatorKeepsStateAcrossRestart(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// Round 1 triggers offline
	snap1 := Snapshot{
		Now: now,
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-5 * time.Minute)},
		},
	}
	events1, nextStates := Evaluate(rules, snap1, nil)
	if len(events1) != 1 {
		t.Fatalf("round 1 expected 1 event, got %d", len(events1))
	}

	// Simulate panel restart: loaded states from database = nextStates
	// Round 2 right after restart (within cooldown) should NOT fire again
	snap2 := Snapshot{
		Now: now.Add(time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-6 * time.Minute)},
		},
	}
	events2, _ := Evaluate(rules, snap2, nextStates)
	if len(events2) != 0 {
		t.Fatalf("expected no duplicate event across restart within cooldown, got %+v", events2)
	}
}
