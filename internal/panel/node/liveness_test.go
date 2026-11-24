package node

import (
	"testing"
	"time"
)

func TestLivenessPendingAndSessionIsolation(t *testing.T) {
	now := time.Now()
	live := NewLiveness(nil)
	session := live.Connect("n", "identity", now)
	hello := live.Receive(session, now)
	if fact := live.Snapshot(now)["n"]; fact.Connected {
		t.Fatal("handshake cannot establish online evidence")
	}
	live.Establish(session)
	live.Complete(session, hello)
	message := live.Receive(session, now)
	later := now.Add(2 * time.Minute)
	fact := live.Snapshot(later)["n"]
	if !fact.Connected || !fact.ProcessingDelayed || fact.InFlightCount != 1 {
		t.Fatalf("pending fact: %+v", fact)
	}
	if ConfirmedOfflineWithLiveness("online", now.Add(-time.Hour).Format(time.RFC3339Nano), 0, later, fact) {
		t.Fatal("pending live message marked offline")
	}
	live.Disconnect(session, later)
	fact = live.Snapshot(later)["n"]
	if !ConfirmedOfflineWithLiveness("online", now.Add(-time.Hour).Format(time.RFC3339Nano), 0, later, fact) {
		t.Fatal("disconnect retained pending privilege")
	}
	replacement := live.Connect("n", "new identity", later)
	live.Establish(replacement)
	live.Receive(replacement, later)
	live.Complete(session, message)
	live.Disconnect(session, later)
	if fact = live.Snapshot(later)["n"]; !fact.Connected || fact.InFlightCount != 1 {
		t.Fatalf("old token changed replacement: %+v", fact)
	}
	live.Invalidate("n")
	live.Receive(replacement, later)
	if _, ok := live.Snapshot(later)["n"]; ok {
		t.Fatal("invalidated session revived")
	}
}

func TestLivenessDisplayKeepsDurableFacts(t *testing.T) {
	now := time.Now()
	live := NewLiveness(nil)
	session := live.Connect("n", "identity", now)
	live.Establish(session)
	live.Receive(session, now.Add(-time.Minute))
	n := Node{ID: "n", Status: "offline", PersistedStatus: "degraded", Enabled: true, LastHeartbeat: now.Add(-time.Hour).Format(time.RFC3339Nano), Liveness: live.Snapshot(now)["n"]}
	if got := DisplayStatus(n, now); got != "degraded" {
		t.Fatalf("display=%s", got)
	}
	if n.Status != "offline" || n.LastHeartbeat != now.Add(-time.Hour).Format(time.RFC3339Nano) {
		t.Fatal("durable evidence changed")
	}
	n.Enabled = false
	if got := DisplayStatus(n, now); got != "disabled" {
		t.Fatal(got)
	}
	n.Enabled = true
	n.Maintenance = true
	if got := DisplayStatus(n, now); got != "maintenance" {
		t.Fatal(got)
	}
	n.Maintenance = false
	n.PersistedStatus = "error"
	if got := DisplayStatus(n, now); got != "error" {
		t.Fatal(got)
	}
}

func TestLivenessReclaimsOnlyExpiredDisconnectedEvidence(t *testing.T) {
	now := time.Now()
	live := NewLiveness(nil)
	session := live.Connect("n", "identity", now)
	live.Establish(session)
	live.Disconnect(session, now)
	live.forgetExpired("n", "online", now.Format(time.RFC3339Nano), 180, now.Add(time.Minute))
	if _, ok := live.Snapshot(now)["n"]; !ok {
		t.Fatal("discarded grace evidence")
	}
	live.forgetExpired("n", "error", now.Format(time.RFC3339Nano), 180, now.Add(4*time.Minute))
	if _, ok := live.Snapshot(now)["n"]; !ok {
		t.Fatal("discarded explicit disconnect that still changes error-state interpretation")
	}
	live.forgetExpired("n", "online", now.Format(time.RFC3339Nano), 180, now.Add(4*time.Minute))
	if _, ok := live.Snapshot(now)["n"]; ok {
		t.Fatal("expired evidence retained")
	}
}
