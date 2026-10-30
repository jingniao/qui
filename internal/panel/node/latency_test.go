package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"testing"
	"time"
)

func TestLatencyStepSeconds(t *testing.T) {
	for _, tt := range []struct{ hours, step int }{{1, 60}, {6, 60}, {12, 60}, {24, 120}, {168, 840}} {
		t.Run(fmt.Sprint(tt.hours), func(t *testing.T) {
			if got := latencyStepSeconds(tt.hours); got != tt.step {
				t.Fatalf("step = %d, want %d", got, tt.step)
			}
		})
	}
}

type latencyTestResponse struct {
	IntervalSeconds int             `json:"intervalSeconds"`
	SampleSeconds   int             `json:"sampleSeconds"`
	Series          []latencySeries `json:"series"`
}

func readLatency(t *testing.T, r *Repository, id string, hours int) latencyTestResponse {
	t.Helper()
	req := httptest.NewRequest("GET", fmt.Sprintf("/?hours=%d", hours), nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	(&Handler{Repository: r}).latency(w, req)
	var result latencyTestResponse
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLatencyBucketsAndRawPoints(t *testing.T) {
	r, id := latencyFixture(t)
	base := time.Now().UTC().Truncate(2 * time.Minute).Add(-10 * time.Minute)
	inputs := []struct {
		offset time.Duration
		median any
		failed int
	}{
		{0, 10.0, 0},
		{time.Minute, 30.0, 1},
		{2*time.Minute - time.Nanosecond, 999.0, 3},
		{2 * time.Minute, 500.0, 3},
		{3 * time.Minute, nil, 3},
		{4 * time.Minute, nil, 1},
	}
	for _, in := range inputs {
		if _, err := r.Database.Exec(`INSERT INTO node_latency_samples
    (node_id,target_id,at,avg_ms,min_ms,max_ms,median_ms,sent,failed)
    VALUES(?,'a',?,11,5,40,?,3,?)`, id, timestamp(base.Add(in.offset)), in.median, in.failed); err != nil {
			t.Fatal(err)
		}
	}
	// A second target must not contribute to the first target's buckets.
	if _, err := r.Database.Exec("INSERT INTO node_latency_samples(node_id,target_id,at,median_ms,sent,failed) VALUES(?,'b',?,80,3,0)", id, timestamp(base)); err != nil {
		t.Fatal(err)
	}
	result := readLatency(t, r, id, 24)
	if result.IntervalSeconds != 120 || result.SampleSeconds != 60 || len(result.Series) != 2 {
		t.Fatal(result)
	}
	points := result.Series[0].Points
	if result.Series[0].TargetID != "a" || len(points) != 3 {
		t.Fatal(result)
	}
	p := points[0]
	if p.At != timestamp(base) || p.Samples != 3 || p.Sent != 9 || p.Failed != 4 || p.AvgMs != nil || p.MedianMs == nil || *p.MedianMs != 20 || p.MinMs == nil || *p.MinMs != 10 || p.MaxMs == nil || *p.MaxMs != 30 {
		t.Fatalf("mixed bucket: %+v", p)
	}
	for i, p := range points[1:] {
		if p.At != timestamp(base.Add(time.Duration(i+1)*2*time.Minute)) || p.AvgMs != nil || p.MedianMs != nil || p.MinMs != nil || p.MaxMs != nil {
			t.Fatalf("empty median bucket: %+v", p)
		}
	}
	if points[1].Samples != 2 || points[1].Sent != 6 || points[1].Failed != 6 || points[2].Samples != 1 || points[2].Sent != 3 || points[2].Failed != 1 {
		t.Fatal(points)
	}
	if p := result.Series[1].Points[0]; p.Samples != 1 || p.MedianMs == nil || *p.MedianMs != 80 {
		t.Fatal(p)
	}
	for _, hours := range []int{1, 6, 12} {
		raw := readLatency(t, r, id, hours)
		if raw.IntervalSeconds != 60 || raw.SampleSeconds != 60 || len(raw.Series[0].Points) != len(inputs) {
			t.Fatal(raw)
		}
		for i, p := range raw.Series[0].Points {
			if p.At != timestamp(base.Add(inputs[i].offset)) || p.Samples != 1 || p.Sent != 3 || p.Failed != inputs[i].failed || p.AvgMs == nil || *p.AvgMs != 11 || p.MinMs == nil || *p.MinMs != 5 || p.MaxMs == nil || *p.MaxMs != 40 {
				t.Fatalf("raw point changed: %+v", p)
			}
			if want, ok := inputs[i].median.(float64); ok {
				if p.MedianMs == nil || *p.MedianMs != want {
					t.Fatal(p)
				}
			} else if p.MedianMs != nil {
				t.Fatal(p)
			}
		}
	}
}

func TestLatencyFullWeekPointLimit(t *testing.T) {
	r, id := latencyFixture(t)
	// One statement populates a full week for both targets without per-row commits.
	start := time.Now().UTC().Truncate(time.Minute).Add(-7*24*time.Hour + time.Minute)
	if _, err := r.Database.Exec(`WITH RECURSIVE minutes(i) AS (
    VALUES(0) UNION ALL SELECT i+1 FROM minutes WHERE i<10079
)
INSERT INTO node_latency_samples(node_id,target_id,at,median_ms,sent,failed)
SELECT ?,t.id,strftime('%Y-%m-%dT%H:%M:%S',?+i*60,'unixepoch')||'.000000000Z',10,3,0
FROM minutes CROSS JOIN probe_targets t WHERE t.id IN ('a','b')`, id, start.Unix()); err != nil {
		t.Fatal(err)
	}
	result := readLatency(t, r, id, 168)
	if result.IntervalSeconds != 840 || result.SampleSeconds != 60 || len(result.Series) != 2 {
		t.Fatal(result)
	}
	for _, s := range result.Series {
		if len(s.Points) < 720 || len(s.Points) > 721 {
			t.Fatalf("%s has %d points", s.TargetID, len(s.Points))
		}
		samples := 0
		for i, p := range s.Points {
			at, err := time.Parse(time.RFC3339Nano, p.At)
			if err != nil || at.Unix()%840 != 0 || (i > 0 && p.At <= s.Points[i-1].At) {
				t.Fatalf("unaligned or unordered bucket: %+v, %v", p, err)
			}
			samples += p.Samples
		}
		if samples != 10080 {
			t.Fatalf("%s has %d raw samples", s.TargetID, samples)
		}
	}
}

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
