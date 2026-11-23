package node

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"
	"time"
)

func compactTelemetryForTest(t *testing.T, r *Repository, now time.Time) {
	t.Helper()
	for {
		more, err := r.CompactTelemetry(context.Background(), now)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			return
		}
	}
}

func TestTelemetryTiersPreserveWeightedMetricsAndCounterBoundary(t *testing.T) {
	r, id := latencyFixture(t)
	// Keep both storage buckets inside one 15-minute display bucket.
	now := time.Now().UTC().Truncate(15 * time.Minute)
	base := now.Add(-25 * time.Hour)
	for i := 0; i < 7; i++ {
		var cpu any
		if i == 0 {
			cpu = 10.0
		}
		if i == 5 {
			cpu = 50.0
		}
		if _, err := r.Database.Exec(`INSERT INTO node_metric_samples(node_id,at,cpu_percent,tcp_out_segs,tcp_retrans_segs) VALUES(?,?,?,?,?)`, id, timestamp(base.Add(time.Duration(i)*time.Minute)), cpu, 1000+i*100, 10+i); err != nil {
			t.Fatal(err)
		}
	}
	compactTelemetryForTest(t, r, now)
	var raw, coarse, samples, measured int
	var sum, min, max float64
	if err := r.Database.QueryRow(`SELECT count(*) FROM node_metric_samples WHERE node_id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := r.Database.QueryRow(`SELECT count(*),sum(samples),sum(cpu_percent_count),sum(cpu_percent_sum),min(cpu_percent_min),max(cpu_percent_max) FROM node_metric_rollups WHERE node_id=?`, id).Scan(&coarse, &samples, &measured, &sum, &min, &max); err != nil {
		t.Fatal(err)
	}
	if raw != 0 || coarse != 2 || samples != 7 || measured != 2 || sum != 60 || min != 10 || max != 50 {
		t.Fatalf("raw=%d coarse=%d samples=%d measured=%d sum=%v range=%v..%v", raw, coarse, samples, measured, sum, min, max)
	}
	var rates, rateSum float64
	if err := r.Database.QueryRow(`SELECT sum(tcp_retrans_percent_count),sum(tcp_retrans_percent_sum) FROM node_metric_rollups WHERE node_id=?`, id).Scan(&rates, &rateSum); err != nil {
		t.Fatal(err)
	}
	if rates != 6 || rateSum != 6 {
		t.Fatalf("lost rate across compacted buckets: count=%v sum=%v", rates, rateSum)
	}
	compactTelemetryForTest(t, r, now)
	req := httptest.NewRequest("GET", "/?hours=168", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	(&Handler{Repository: r}).metrics(w, req)
	var result struct {
		IntervalSeconds int           `json:"intervalSeconds"`
		Points          []MetricPoint `json:"points"`
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.IntervalSeconds != 900 {
		t.Fatalf("coarse interval %d", result.IntervalSeconds)
	}
	var count int
	var cpuPoints int
	for _, p := range result.Points {
		count += p.Samples
		if p.CPUPercent != nil {
			cpuPoints++
			if math.Abs(*p.CPUPercent-30) > 1e-9 {
				t.Fatalf("unweighted CPU: %+v", p)
			}
		}
		if p.MemoryPercent != nil {
			t.Fatal("missing memory became measured")
		}
	}
	if count != 7 || cpuPoints != 1 {
		t.Fatal(result)
	}
}

func TestTelemetryTiersPreserveLatencyWeightsNullAndRetention(t *testing.T) {
	r, id := latencyFixture(t)
	now := time.Now().UTC().Truncate(5 * time.Minute)
	base := now.Add(-25 * time.Hour)
	for i, median := range []any{10.0, 30.0, nil, 90.0} {
		failed := 0
		if i == 2 {
			failed = 3
		}
		if _, err := r.Database.Exec(`INSERT INTO node_latency_samples(node_id,target_id,at,median_ms,sent,failed) VALUES(?,'a',?,?,3,?)`, id, timestamp(base.Add(time.Duration(i)*time.Minute)), median, failed); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Database.Exec(`INSERT INTO node_latency_samples(node_id,target_id,at,median_ms,sent,failed) VALUES(?,'b',?,500,3,0),(?,'a',?,22,3,0),(?,'a',?,100,3,0)`, id, timestamp(base), id, timestamp(now.Add(-time.Hour)), id, timestamp(now.Add(-8*24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	compactTelemetryForTest(t, r, now)
	var raw int
	if err := r.Database.QueryRow(`SELECT count(*) FROM node_latency_samples WHERE node_id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != 1 {
		t.Fatalf("raw=%d", raw)
	}
	var samples, sent, failed, valid int
	var sum, min, max float64
	if err := r.Database.QueryRow(`SELECT samples,sent,failed,median_count,median_sum,median_min,median_max FROM node_latency_rollups WHERE node_id=? AND target_id='a'`, id).Scan(&samples, &sent, &failed, &valid, &sum, &min, &max); err != nil {
		t.Fatal(err)
	}
	if samples != 4 || sent != 12 || failed != 3 || valid != 3 || sum != 130 || min != 10 || max != 90 {
		t.Fatalf("%d %d %d %d %v %v %v", samples, sent, failed, valid, sum, min, max)
	}
	result := readLatency(t, r, id, 168)
	if result.IntervalSeconds != 900 {
		t.Fatal(result)
	}
	if len(result.Series) < 2 || result.Series[0].Points[0].MedianMs == nil || math.Abs(*result.Series[0].Points[0].MedianMs-130.0/3) > 1e-9 {
		t.Fatal(result)
	}
}

func TestTelemetryMetricIntegerMeansRemainFractional(t *testing.T) {
	r, id := latencyFixture(t)
	base := time.Now().UTC().Truncate(2 * time.Minute).Add(-10 * time.Minute)
	for i := 0; i < 2; i++ {
		if _, err := r.Database.Exec(`INSERT INTO node_metric_samples(node_id,at,process_count) VALUES(?,?,?)`, id, timestamp(base.Add(time.Duration(i)*time.Minute)), i+1); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest("GET", "/?hours=24", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	(&Handler{Repository: r}).metrics(w, req)
	var result struct {
		Points []MetricPoint `json:"points"`
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Points) != 1 || result.Points[0].ProcessCount == nil || *result.Points[0].ProcessCount != 1.5 {
		t.Fatalf("integer mean lost precision: %+v", result)
	}
}
