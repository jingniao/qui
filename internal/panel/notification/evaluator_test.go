package notification

import (
	nodeavailability "proxycontrol/internal/panel/node"
	"strings"
	"testing"
	"time"
)

func TestOfflineEvaluationUsesTransportAndResolvesDelayedRecovery(t *testing.T) {
	now := time.Now()
	live := nodeavailability.NewLiveness(nil)
	session := live.Connect("n", "identity", now.Add(-time.Minute))
	live.Establish(session)
	live.Receive(session, now.Add(-time.Minute))
	snapshot := Snapshot{Now: now, Nodes: []NodeSnapshot{{ID: "n", Name: "node", Status: "offline", LastHeartbeatRaw: now.Add(-time.Hour).Format(time.RFC3339Nano), Liveness: live.Snapshot(now)["n"]}}}
	rules := []Rule{{Kind: "node_offline", Enabled: true, DurationMinutes: 0}}
	events, _ := Evaluate(rules, snapshot, map[string]State{})
	if len(events) != 0 {
		t.Fatalf("pending transport triggered offline: %+v", events)
	}
	events, states := Evaluate(rules, snapshot, map[string]State{"node_offline:n": {Active: true}})
	if len(events) != 1 || !events[0].Resolved || !strings.Contains(events[0].Body, "处理仍有延迟") || len(states) != 0 {
		t.Fatalf("recovery: %+v %+v", events, states)
	}
	live.Disconnect(session, now)
	snapshot.Nodes[0].Liveness = live.Snapshot(now)["n"]
	snapshot.Nodes[0].Status = "online"
	events, _ = Evaluate(rules, snapshot, map[string]State{})
	if len(events) != 1 || events[0].Resolved {
		t.Fatalf("disconnect hidden by old database online: %+v", events)
	}
	snapshot.Nodes[0].Maintenance = true
	events, states = Evaluate(rules, snapshot, map[string]State{"node_offline:n": {Active: true}})
	if len(events) != 0 || !states["node_offline:n"].Active {
		t.Fatal("maintenance changed episode")
	}
}

func floatPtr(v float64) *float64 { return &v }
func uint64Ptr(v uint64) *uint64  { return &v }

func TestOfflineStartupObservation(t *testing.T) {
	start := time.Now().UTC()
	oldHeartbeat := start.Add(-time.Hour).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name       string
		status     string
		heartbeat  string
		after      time.Duration
		active     bool
		wantEvents int
	}{
		{"restart", "offline", oldHeartbeat, 0, false, 0},
		{"still_observing", "offline", oldHeartbeat, 3*time.Minute - time.Nanosecond, false, 0},
		{"really_offline", "offline", oldHeartbeat, 3 * time.Minute, false, 1},
		{"reconnected", "online", start.Format(time.RFC3339Nano), time.Second, false, 0},
		{"existing_offline", "offline", oldHeartbeat, time.Second, true, 0},
		{"existing_recovery", "online", start.Format(time.RFC3339Nano), time.Second, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states := map[string]State{}
			if tc.active {
				states["node_offline:n"] = State{Key: "node_offline:n", Active: true, Since: start.Add(-time.Hour)}
			}
			events, next := Evaluate(defaultRules(), Snapshot{Now: start.Add(tc.after), OfflineAlertNotBefore: start.Add(3 * time.Minute), Nodes: []NodeSnapshot{{ID: "n", Status: tc.status, LastHeartbeatRaw: tc.heartbeat}}}, states)
			if len(events) != tc.wantEvents {
				t.Fatalf("events = %+v", events)
			}
			if tc.active && tc.status == "offline" && !next["node_offline:n"].Active {
				t.Fatal("existing offline episode lost")
			}
			if tc.name == "existing_recovery" && (!events[0].Resolved || next["node_offline:n"].Active) {
				t.Fatal("recovery blocked during startup observation")
			}
			if !tc.active && tc.wantEvents == 0 && next["node_offline:n"].Active {
				t.Fatal("observation must not create a fake offline episode")
			}
		})
	}
}

func TestOfflineAvailabilityFlagsAndRawHeartbeat(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, status, heartbeat string
		maintenance, active     bool
		wantEvents              int
	}{
		{"maintenance", "offline", "", true, false, 0},
		{"maintenance_recovery", "online", now.Format(time.RFC3339Nano), true, true, 0},
		{"disabled", "disabled", "", false, false, 0},
		{"missing", "online", "", false, false, 1},
		{"invalid", "online", "invalid", false, false, 1},
		{"future", "online", now.Add(time.Hour).Format(time.RFC3339Nano), false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states := map[string]State{}
			if tc.active {
				states["node_offline:n"] = State{Key: "node_offline:n", Active: true}
			}
			events, next := Evaluate(defaultRules(), Snapshot{Now: now, Nodes: []NodeSnapshot{{ID: "n", Status: tc.status, LastHeartbeatRaw: tc.heartbeat, Maintenance: tc.maintenance}}}, states)
			if len(events) != tc.wantEvents {
				t.Fatalf("events = %+v", events)
			}
			if tc.wantEvents == 1 && events[0].Body != "没有有效心跳记录" {
				t.Fatalf("invalid heartbeat body = %q", events[0].Body)
			}
			if tc.active && !next["node_offline:n"].Active {
				t.Fatal("maintenance must preserve the offline episode")
			}
		})
	}
}

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
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-2 * time.Minute), LastHeartbeatRaw: now.Add(-2 * time.Minute).Format(time.RFC3339Nano)},
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
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-2 * time.Minute), LastHeartbeatRaw: now.Add(-2 * time.Minute).Format(time.RFC3339Nano)},
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
			{ID: "node1", Name: "Tokyo", Status: "online", LastHeartbeat: now.Add(2 * time.Minute), LastHeartbeatRaw: now.Add(2 * time.Minute).Format(time.RFC3339Nano)},
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
			{ID: "node_disabled", Name: "DisabledNode", Status: "disabled", LastHeartbeat: now.Add(-10 * time.Minute), LastHeartbeatRaw: now.Add(-10 * time.Minute).Format(time.RFC3339Nano)},
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
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online", LastHeartbeatRaw: now.Format(time.RFC3339Nano)}},
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
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online", LastHeartbeatRaw: now.Add(time.Minute).Format(time.RFC3339Nano)}},
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
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online", LastHeartbeatRaw: now.Add(2 * time.Minute).Format(time.RFC3339Nano)}},
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
		Nodes: []NodeSnapshot{{ID: "node1", Name: "UK", Status: "online", LastHeartbeatRaw: now.Format(time.RFC3339Nano)}},
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
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-5 * time.Minute), LastHeartbeatRaw: now.Add(-5 * time.Minute).Format(time.RFC3339Nano)},
		},
	}
	events, states := Evaluate(rules, snap1, nil)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// 2. Still in the same offline episode -> no repeat
	snap2 := Snapshot{
		Now: now.Add(10 * time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-15 * time.Minute), LastHeartbeatRaw: now.Add(-15 * time.Minute).Format(time.RFC3339Nano)},
		},
	}
	events, states = Evaluate(rules, snap2, states)
	if len(events) != 0 {
		t.Fatalf("expected the active offline episode to suppress repeat alert, got %+v", events)
	}

	// 3. The cooldown has expired, but the same offline episode still fires once
	snap3 := Snapshot{
		Now: now.Add(31 * time.Minute),
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-36 * time.Minute), LastHeartbeatRaw: now.Add(-36 * time.Minute).Format(time.RFC3339Nano)},
		},
	}
	events, states = Evaluate(rules, snap3, states)
	if len(events) != 0 {
		t.Fatalf("expected no repeated offline alert after cooldown expiry, got %+v", events)
	}
}

func TestPublicIPChangeFirstSightIsSilent(t *testing.T) {
	rules := defaultRules()
	now := time.Now().UTC()

	// 1. First sight: records state silently
	snap1 := Snapshot{
		Now: now,
		Nodes: []NodeSnapshot{
			{ID: "node1", Name: "Alpine", Status: "online", LastHeartbeatRaw: now.Format(time.RFC3339Nano), Addresses: []string{"203.0.113.5"}},
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
			{ID: "node1", Name: "Alpine", Status: "online", LastHeartbeatRaw: now.Add(time.Minute).Format(time.RFC3339Nano), Addresses: []string{"203.0.113.9"}},
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
		Nodes: []NodeSnapshot{{ID: "node1", Name: "HK", Status: "online", LastHeartbeatRaw: now.Format(time.RFC3339Nano)}},
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
		Nodes: []NodeSnapshot{{ID: "node1", Name: "HK", Status: "online", LastHeartbeatRaw: now.Add(time.Hour).Format(time.RFC3339Nano)}},
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
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-5 * time.Minute), LastHeartbeatRaw: now.Add(-5 * time.Minute).Format(time.RFC3339Nano)},
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
			{ID: "node1", Name: "Tokyo", Status: "offline", LastHeartbeat: now.Add(-6 * time.Minute), LastHeartbeatRaw: now.Add(-6 * time.Minute).Format(time.RFC3339Nano)},
		},
	}
	events2, _ := Evaluate(rules, snap2, nextStates)
	if len(events2) != 0 {
		t.Fatalf("expected no duplicate event across restart within cooldown, got %+v", events2)
	}
}
