package user

import (
	"context"
	"testing"
	"time"
)

func TestInactivityReasonPrecedence(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	past := timestamp(at.Add(-time.Hour))
	for _, tt := range []struct {
		name string
		user User
		want string
	}{
		{"active", User{Status: "active"}, ""},
		{"disabled", User{Status: "disabled"}, "disabled"},
		{"quota", User{Status: "active", TrafficLimit: 100, UsedTraffic: 100}, "quota_exceeded"},
		{"expired", User{Status: "active", ExpiresAt: past}, "expired"},
	} {
		if reason := InactivityReason(tt.user, at); reason != tt.want {
			t.Fatalf("%s: got %q want %q", tt.name, reason, tt.want)
		}
		if active := tt.user.Active(at); active != (tt.want == "") {
			t.Fatalf("%s: Active disagrees with InactivityReason", tt.name)
		}
	}
}

func TestQuotaStateSeedThenReportEachChangeOnce(t *testing.T) {
	ctx := context.Background()
	repository := testRepository(t)
	active, err := repository.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	overQuota, err := repository.Save(ctx, "", Input{Username: "quota_user", DisplayName: "Quota", Status: "active", TrafficLimit: 100, MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Database.ExecContext(ctx, "UPDATE users SET used_traffic=100 WHERE id=?", overQuota.ID); err != nil {
		t.Fatal(err)
	}
	state := NewQuotaState(repository)
	state.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := state.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	// Seeding from the database must not report the already-over-quota user:
	// a restart alone must never trigger an automatic deploy.
	if transitions, err := state.Evaluate(ctx, []string{active.ID, overQuota.ID}); err != nil || len(transitions) != 0 {
		t.Fatalf("seeded evaluation reported transitions: %v %v", transitions, err)
	}
	// A real crossing reports exactly once, and repeated reports stay silent.
	if _, err := repository.Database.ExecContext(ctx, "UPDATE users SET traffic_limit=100,used_traffic=500 WHERE id=?", active.ID); err != nil {
		t.Fatal(err)
	}
	transitions, err := state.Evaluate(ctx, []string{active.ID})
	if err != nil || len(transitions) != 1 || transitions[0].Reason != "quota_exceeded" || transitions[0].Username != "test_user" {
		t.Fatalf("crossing: %v %v", transitions, err)
	}
	if transitions, err := state.Evaluate(ctx, []string{active.ID}); err != nil || len(transitions) != 0 {
		t.Fatalf("repeated report produced transitions: %v %v", transitions, err)
	}
	// Raising the quota reports one reactivation.
	if _, err := repository.Database.ExecContext(ctx, "UPDATE users SET traffic_limit=1000 WHERE id=?", active.ID); err != nil {
		t.Fatal(err)
	}
	transitions, err = state.Evaluate(ctx, []string{active.ID})
	if err != nil || len(transitions) != 1 || transitions[0].Reason != "reactivated" {
		t.Fatalf("reactivation: %v %v", transitions, err)
	}
}
