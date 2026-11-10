package notification

import (
	"context"
	"testing"
	"time"
)

func TestRecoveryOnlyOnceAfterStateRoundTrip(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()
	states := map[string]State{"node_offline:n": {Key: "node_offline:n", Active: true, Since: now, LastNotified: now}}
	if err := repo.SaveStates(ctx, states); err != nil {
		t.Fatal(err)
	}
	rules := []Rule{{Kind: "node_offline", Enabled: true}}
	snap := Snapshot{Now: now, Nodes: []NodeSnapshot{{ID: "n", Status: "online"}}}
	for round := 0; round < 2; round++ {
		stored, err := repo.States(ctx)
		if err != nil {
			t.Fatal(err)
		}
		events, next := Evaluate(rules, snap, stored)
		if len(events) != 1-round {
			t.Fatalf("round %d: got %d recoveries", round, len(events))
		}
		if err := repo.SaveStates(ctx, next); err != nil {
			t.Fatal(err)
		}
	}
}

type durationSources struct {
	t           *testing.T
	wantMinutes int
}

func (s durationSources) Nodes(context.Context) ([]NodeSnapshot, error) {
	return []NodeSnapshot{{ID: "n", Status: "online"}}, nil
}
func (s durationSources) Traffic(context.Context, time.Time) (map[string]TrafficSnapshot, error) {
	return nil, nil
}
func (s durationSources) Metrics(_ context.Context, since time.Time) (map[string]MetricWindow, error) {
	s.t.Helper()
	want := time.Now().Truncate(time.Minute).Add(-time.Duration(s.wantMinutes) * time.Minute)
	if !since.Equal(want) {
		s.t.Errorf("since=%v, want %v", since, want)
	}
	v := 95.0
	samples := make([]*float64, int(time.Since(since)/time.Minute))
	for i := range samples {
		samples[i] = &v
	}
	return map[string]MetricWindow{"n": {NodeID: "n", CPU: samples}}, nil
}

func TestWorkerUsesLongestEnabledResourceDuration(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	for _, tc := range []struct {
		cpu, memory, want int
		memoryEnabled     bool
	}{
		{30, 60, 30, false}, {30, 60, 60, true}, {5, 10, 15, true},
	} {
		rules := []Rule{{Kind: "cpu", Enabled: true, Threshold: 90, DurationMinutes: tc.cpu}, {Kind: "memory", Enabled: tc.memoryEnabled, Threshold: 90, DurationMinutes: tc.memory}, {Kind: "node_offline", Enabled: true, DurationMinutes: 1440}}
		if err := repo.SaveRules(ctx, rules); err != nil {
			t.Fatal(err)
		}
		worker := Worker{Repository: repo, Sources: durationSources{t, tc.want}}
		worker.tick(ctx)
	}
	_, count, err := repo.List(ctx, false, false, 1, 100)
	if err != nil || count < 1 {
		t.Fatalf("30 minute CPU rule did not fire: %d %v", count, err)
	}
}

func TestPublicIPCooldownAndEmptyCandidates(t *testing.T) {
	now := time.Now()
	rules := []Rule{{Kind: "public_ip", Enabled: true, CooldownMinutes: 30}}
	snap := Snapshot{Now: now, Nodes: []NodeSnapshot{{ID: "n", Status: "online", Addresses: []string{"1.1.1.1"}}}}
	_, states := Evaluate(rules, snap, nil)
	for i, tc := range []struct {
		addresses []string
		minutes   int
		want      int
	}{
		{[]string{"8.8.8.8"}, 1, 1},
		{[]string{"9.9.9.9"}, 2, 0},
		{nil, 31, 0},
		{[]string{"9.9.9.9"}, 32, 1},
	} {
		snap.Now = now.Add(time.Duration(tc.minutes) * time.Minute)
		snap.Nodes[0].Addresses = tc.addresses
		var events []Event
		events, states = Evaluate(rules, snap, states)
		if len(events) != tc.want {
			t.Errorf("step %d: got %d, want %d", i, len(events), tc.want)
		}
	}
}
