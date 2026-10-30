package node

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"testing"
	"time"
)

func latencyFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(context.Background(), "latency", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES('a','A','1.1.1.1',443,1),('b','B','1.0.0.1',443,1),('manual','M','8.8.8.8',443,0)",
		"UPDATE nodes SET tcp_probe_supported=1,last_heartbeat='" + timestamp(time.Now().UTC()) + "'",
	} {
		if _, err = database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return r, n.ID
}

func probeCount(t *testing.T, r *Repository, query string, args ...any) int {
	t.Helper()
	var count int
	if err := r.Database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestLatencyEligibilityQueueAndRetention(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, q := range []string{"enabled=0", "deleted_at='2026-01-01T00:00:00Z'", "tcp_probe_supported=0", "last_heartbeat='2000-01-01T00:00:00Z'", "last_heartbeat='" + timestamp(now.Add(-46*time.Second)) + "'"} {
		n, _, _, err := r.CreateEnrollment(ctx, "ineligible"+string(rune('a'+i)), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Database.Exec("UPDATE nodes SET tcp_probe_supported=1,last_heartbeat=? WHERE id=?", timestamp(now), n.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = r.Database.Exec("UPDATE nodes SET "+q+" WHERE id=?", n.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Database.Exec("INSERT INTO node_latency_samples(node_id,target_id,at,sent,failed) VALUES(?,'a',?,3,3),(?,'a',?,3,3)", id, timestamp(now.Add(-7*24*time.Hour-time.Second)), id, timestamp(now.Add(-7*24*time.Hour+time.Second))); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.scheduleLatency(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes"); n != 2 {
		t.Fatal("wrong eligibility or duplicate queue", n)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes WHERE node_id=? AND source='scheduled'", id); n != 2 {
		t.Fatal(n)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_samples"); n != 1 {
		t.Fatal("7 day retention", n)
	}
	c, err := r.pendingTCPProbe(ctx, id)
	if err != nil || c == nil || c.Attempts != 3 || c.TimeoutMs != 3000 {
		t.Fatal(c, err)
	}
	next, err := r.pendingTCPProbe(ctx, id)
	if err != nil || next.Id != c.Id {
		t.Fatal("concurrent probe", next, err)
	}
}

func TestScheduledResultValidationLossAndManualPriority(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	if err := r.scheduleLatency(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Repository: r}
	manual := func() string {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`{"targetId":"manual","attempts":3,"timeoutMs":100}`))
		req.SetPathValue("id", id)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.tcpProbes(w, req)
		if w.Code != 202 {
			t.Fatal("manual blocked by scheduled", w.Code, w.Body.String())
		}
		var result map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result["id"]
	}
	manualID := manual()
	c, err := r.pendingTCPProbe(ctx, id)
	if err != nil || c.Id != manualID {
		t.Fatal("queued manual lacks priority", c, err)
	}
	if err = r.recordTCPProbe(ctx, id, &pb.TCPProbeResult{Id: c.Id, Attempted: 3, Lost: 3, Code: "complete"}); err != nil {
		t.Fatal(err)
	}
	c, err = r.pendingTCPProbe(ctx, id)
	if err != nil || c == nil {
		t.Fatal(c, err)
	}
	manualID = manual()
	next, err := r.pendingTCPProbe(ctx, id)
	if err != nil || next.Id != c.Id {
		t.Fatal("in-flight scheduled was superseded", next, err)
	}
	bad := &pb.TCPProbeResult{Id: c.Id, Attempted: 4, Lost: 4, Code: "complete"}
	if r.recordTCPProbe(ctx, id, bad) == nil {
		t.Fatal("ValidateResult bypassed")
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_samples"); n != 0 {
		t.Fatal(n)
	}
	bad.Attempted = 3
	bad.Lost = 3
	for i := 0; i < 2; i++ {
		if err = r.recordTCPProbe(ctx, id, bad); err != nil {
			t.Fatal(err)
		}
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_samples WHERE sent=3 AND failed=3 AND avg_ms IS NULL AND median_ms IS NULL"); n != 1 {
		t.Fatal("loss sample or duplicate", n)
	}
	c, err = r.pendingTCPProbe(ctx, id)
	if err != nil || c.Id != manualID {
		t.Fatal("manual not next", c, err)
	}
	if err = r.recordTCPProbe(ctx, id, &pb.TCPProbeResult{Id: c.Id, Attempted: 3, Lost: 3, Code: "complete"}); err != nil {
		t.Fatal(err)
	}
	c, err = r.pendingTCPProbe(ctx, id)
	if err != nil || c == nil {
		t.Fatal(c, err)
	}
	ms := 12.5
	if err = r.recordTCPProbe(ctx, id, &pb.TCPProbeResult{Id: c.Id, Attempted: 3, Lost: 1, MedianMs: &ms, Code: "complete"}); err != nil {
		t.Fatal(err)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_samples WHERE median_ms=12.5 AND avg_ms IS NULL AND sent=3 AND failed=1"); n != 1 {
		t.Fatal(n)
	}
	req := httptest.NewRequest("GET", "/?targetId=a", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.tcpProbes(w, req)
	if w.Code != 200 || w.Body.String() != "{\"items\":[]}\n" {
		t.Fatal("scheduled leaked into history", w.Body.String())
	}
	if _, err = r.Database.Exec("DELETE FROM probe_targets WHERE id='a'"); err != nil {
		t.Fatal(err)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_samples WHERE target_id='a'"); n != 0 {
		t.Fatal("target samples not cascaded", n)
	}
}

func TestLatencySchedulerSingleLoopAndStop(t *testing.T) {
	r, _ := latencyFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &LatencyScheduler{Repository: r, Interval: 5 * time.Millisecond}
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !s.running.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	second := make(chan struct{})
	go func() { s.Run(ctx); close(second) }()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("second loop started")
	}
	for probeCount(t, r, "SELECT count(*) FROM node_tcp_probes") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if probeCount(t, r, "SELECT count(*) FROM node_tcp_probes") == 0 {
		t.Fatal("loop did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler failed to stop")
	}
	if s.running.Load() {
		t.Fatal("scheduler still running")
	}
	if _, err := r.Database.Exec("DELETE FROM node_tcp_probes"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes"); n != 0 {
		t.Fatal("scheduled after stop", n)
	}
}
