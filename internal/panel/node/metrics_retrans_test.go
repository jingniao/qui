package node

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetransRate(t *testing.T) {
	r, id := latencyFixture(t)
	base := time.Now().UTC().Truncate(2 * time.Minute).Add(-10 * time.Minute)
	samples := []struct {
		out     any
		retrans any
	}{
		{int64(1000), int64(10)},
		{int64(2000), int64(25)}, // delta out=1000, delta retrans=15 => 1.5%
		{int64(3000), int64(30)}, // delta out=1000, delta retrans=5  => 0.5%
	}
	for i, s := range samples {
		at := timestamp(base.Add(time.Duration(i) * time.Minute))
		if _, err := r.Database.Exec(
			"INSERT INTO node_metric_samples(node_id,at,tcp_out_segs,tcp_retrans_segs) VALUES(?,?,?,?)",
			id, at, s.out, s.retrans,
		); err != nil {
			t.Fatal(err)
		}
	}

	read := func(hours int) []MetricPoint {
		req := httptest.NewRequest("GET", fmt.Sprintf("/?hours=%d", hours), nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		(&Handler{Repository: r}).metrics(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var v struct {
			Points []MetricPoint `json:"points"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.Points
	}

	pts := read(12)
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if pts[0].TCPRetransPercent != nil {
		t.Fatalf("first point without predecessor should be nil, got %v", *pts[0].TCPRetransPercent)
	}
	if pts[1].TCPRetransPercent == nil || math.Abs(*pts[1].TCPRetransPercent-1.5) > 1e-6 {
		t.Fatalf("expected second point 1.5%%, got %v", pts[1].TCPRetransPercent)
	}
	if pts[2].TCPRetransPercent == nil || math.Abs(*pts[2].TCPRetransPercent-0.5) > 1e-6 {
		t.Fatalf("expected third point 0.5%%, got %v", pts[2].TCPRetransPercent)
	}

	// 24h view uses 120s buckets: bucket 0 has pts[0] (null) and pts[1] (1.5) => avg 1.5; bucket 1 has pts[2] (0.5) => avg 0.5
	bpts := read(24)
	if len(bpts) != 2 {
		t.Fatalf("expected 2 bucketed points, got %d", len(bpts))
	}
	if bpts[0].TCPRetransPercent == nil || math.Abs(*bpts[0].TCPRetransPercent-1.5) > 1e-6 {
		t.Fatalf("expected first bucket 1.5%%, got %v", bpts[0].TCPRetransPercent)
	}
	if bpts[1].TCPRetransPercent == nil || math.Abs(*bpts[1].TCPRetransPercent-0.5) > 1e-6 {
		t.Fatalf("expected second bucket 0.5%%, got %v", bpts[1].TCPRetransPercent)
	}
}

func TestRetransRateResetIsNull(t *testing.T) {
	r, id := latencyFixture(t)
	base := time.Now().UTC().Truncate(2 * time.Minute).Add(-10 * time.Minute)
	samples := []struct {
		offsetMin int
		out       any
		retrans   any
	}{
		{0, int64(5000), int64(50)},
		{1, int64(1000), int64(5)},  // reboot / counter reset: out decreased => null
		{2, int64(1000), int64(5)},  // delta out == 0 => null
		{3, int64(2000), int64(3)},  // retrans decreased => null
		{4, nil, nil},               // missing measurement => null
		{5, int64(3000), int64(10)}, // previous minute had nil => null
		{7, int64(4000), int64(20)}, // minute 6 skipped (gap > 60s) => null
	}
	for _, s := range samples {
		at := timestamp(base.Add(time.Duration(s.offsetMin) * time.Minute))
		if _, err := r.Database.Exec(
			"INSERT INTO node_metric_samples(node_id,at,tcp_out_segs,tcp_retrans_segs) VALUES(?,?,?,?)",
			id, at, s.out, s.retrans,
		); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest("GET", "/?hours=12", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	(&Handler{Repository: r}).metrics(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct {
		Points []MetricPoint `json:"points"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Points) != len(samples) {
		t.Fatalf("expected %d points, got %d", len(samples), len(v.Points))
	}
	for i, pt := range v.Points {
		if pt.TCPRetransPercent != nil {
			t.Fatalf("point %d (%s) expected nil TCPRetransPercent, got %v", i, pt.At, *pt.TCPRetransPercent)
		}
	}
}
