package node

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http/httptest"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"testing"
	"time"
)

func TestK2bRenewalWindows(t *testing.T) {
	now := time.Now()
	leaf := &x509.Certificate{NotBefore: now.Add(-30 * 24 * time.Hour), NotAfter: now.Add(60 * 24 * time.Hour)}
	for _, tt := range []struct {
		at   time.Time
		want bool
	}{{now.Add(-time.Nanosecond), false}, {now, true}, {now.Add(time.Nanosecond), true}} {
		if certificateRenewalDue(leaf, tt.at) != tt.want {
			t.Fatal(tt)
		}
	}
	leaf.NotBefore = now.Add(-time.Hour)
	leaf.NotAfter = now.Add(14 * 24 * time.Hour)
	if !certificateRenewalDue(leaf, now) || certificateRenewalDue(leaf, now.Add(-time.Nanosecond)) {
		t.Fatal("expiry boundary")
	}
}

func TestK2bFreshMetricsAndViews(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute).Add(20 * time.Second)
	h := &pb.Heartbeat{CpuPercent: proto.Float64(0), ProcessCount: proto.Uint64(0), MemoryUsedBytes: proto.Uint64(123), TuningCurrent: map[string]string{"net.core.somaxconn": "128"}, Interfaces: []*pb.InterfaceCounters{{Name: "eth0", ReceiveBytesPerSecond: proto.Float64(12)}, {Name: "eth1", ReceiveBytesPerSecond: proto.Float64(3)}}}
	m := &pb.AgentMessage{NodeId: id, Sequence: 1, MessageId: "hb", Timestamp: timestamppb.New(now), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: h}}
	// Exercise the actual acceptance transaction, using a real fresh timestamp.
	m.Timestamp = timestamppb.Now()
	if _, err := r.Record(ctx, id, m); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(ctx, id, m); err != nil {
		t.Fatal(err)
	}
	if got := probeCount(t, r, "SELECT count(*) FROM node_metric_samples"); got != 1 {
		t.Fatal(got)
	}
	m.Sequence++
	m.MessageId = "stale"
	m.Timestamp = timestamppb.New(time.Now().Add(-time.Hour))
	h.CpuPercent = proto.Float64(90)
	if _, err := r.Record(ctx, id, m); err != nil {
		t.Fatal(err)
	}
	var cpu, rx float64
	var swap *int64
	if err := r.Database.QueryRow("SELECT cpu_percent,rx_bytes_per_second,swap_used_bytes FROM node_metric_samples").Scan(&cpu, &rx, &swap); err != nil || cpu != 0 || rx != 15 || swap != nil {
		t.Fatal(cpu, rx, swap, err)
	}
	m.Sequence++
	m.MessageId = "caps"
	m.Payload = &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{Host: &pb.HostInfo{CpuModel: "test", MemoryTotalBytes: 123}, TuningSupported: true}}
	if _, err := r.Record(ctx, id, m); err != nil {
		t.Fatal(err)
	}
	n, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"tuningSupported":true`, `"cpuModel":"test"`, `"memoryTotalBytes":"123"`, `"processCount":"0"`, `"tuningCurrent"`} {
		if !strings.Contains(string(raw), part) {
			t.Fatal(part, string(raw))
		}
	}
	if strings.Contains(string(raw), `"swapUsedBytes"`) {
		t.Fatal("missing became zero", string(raw))
	}
}

func TestK2bMetricBucketsAndRetention(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(2 * time.Minute).Add(-10 * time.Minute)
	for i := 0; i < 3; i++ {
		var value any
		if i < 2 {
			value = float64(i * 20)
		}
		if _, err := r.Database.Exec("INSERT INTO node_metric_samples(node_id,at,cpu_percent,process_count) VALUES(?,?,?,?)", id, timestamp(base.Add(time.Duration(i)*time.Minute)), value, value); err != nil {
			t.Fatal(err)
		}
	}
	read := func(hours int) (int, []MetricPoint) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/?hours=%d", hours), nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		(&Handler{Repository: r}).metrics(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var v struct {
			Interval int           `json:"intervalSeconds"`
			Points   []MetricPoint `json:"points"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.Interval, v.Points
	}
	step, p := read(12)
	if step != 60 || len(p) != 3 || p[2].CPUPercent != nil {
		t.Fatal(step, p)
	}
	step, p = read(24)
	if step != 120 || len(p) != 2 || p[0].Samples != 2 || *p[0].CPUPercent != 10 || *p[0].ProcessCount != 10 || p[1].CPUPercent != nil {
		t.Fatal(step, p)
	}
	if _, err := r.Database.Exec("INSERT INTO node_metric_samples(node_id,at) VALUES(?,?)", id, timestamp(time.Now().Add(-8*24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	// Disable network probes; retention uses the existing scheduler lifecycle.
	r.Database.Exec("UPDATE probe_targets SET scheduled=0")
	if err := r.scheduleLatency(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if probeCount(t, r, "SELECT count(*) FROM node_metric_samples") != 3 {
		t.Fatal("retention")
	}
}

func TestK2bTuningWhitelistAndReceipts(t *testing.T) {
	for key, rule := range tuningRules {
		if rule.choices != "" {
			for _, v := range strings.Fields(rule.choices) {
				if err := ValidateTuning(map[string]string{key: v}, false); err != nil {
					t.Fatal(err)
				}
			}
			continue
		}
		for _, v := range []int64{rule.min, rule.max} {
			if err := ValidateTuning(map[string]string{key: fmt.Sprint(v)}, false); err != nil {
				t.Fatal(err)
			}
		}
		for _, v := range []string{fmt.Sprint(rule.min - 1), fmt.Sprint(rule.max + 1), "1 2 4194304", "1.0", "+128", " 128"} {
			if ValidateTuning(map[string]string{key: v}, false) == nil {
				t.Fatal(key, v)
			}
		}
	}
	for _, s := range []map[string]string{{"kernel.secret": "1"}, {"net.ipv4.tcp_congestion_control": "bbr x"}, {"net.core.default_qdisc": "fq;id"}} {
		if ValidateTuning(s, false) == nil {
			t.Fatal(s)
		}
	}
	if ValidateTuning(map[string]string{"net.core.somaxconn": "128"}, true) == nil {
		t.Fatal("clear with settings")
	}
	r, id := latencyFixture(t)
	ctx := context.Background()
	stamp := timestamp(time.Now())
	if _, err := r.Database.Exec("INSERT INTO node_tuning VALUES(?,?,'req','pending',NULL,?,?)", id, `{"net.core.somaxconn":"128"}`, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	c, err := r.pendingTuning(ctx, id)
	if err != nil || c.RequestId != "req" || c.Settings["net.core.somaxconn"] != "128" {
		t.Fatal(c, err)
	}
	if err = r.unconfirmTuning(ctx, id); err != nil {
		t.Fatal(err)
	}
	result := &pb.TuningResult{RequestId: "wrong", Status: "applied", Keys: []*pb.TuningKeyResult{{Key: "net.core.somaxconn", Value: "128", Applied: true}}}
	m := &pb.AgentMessage{NodeId: id, Sequence: 1, MessageId: "result", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_TuningResult{TuningResult: result}}
	if _, err = r.Record(ctx, id, m); err == nil {
		t.Fatal("mismatch accepted")
	}
	result.RequestId = "req"
	if _, err = r.Record(ctx, id, m); err != nil {
		t.Fatal(err)
	}
	m.Sequence++
	m.MessageId = "duplicate"
	if _, err = r.Record(ctx, id, m); err != nil {
		t.Fatal(err)
	}
	result.Status = "failed"
	result.Keys[0].Applied = false
	m.Sequence++
	m.MessageId = "conflict"
	if _, err = r.Record(ctx, id, m); err == nil {
		t.Fatal("conflict accepted")
	}
}
