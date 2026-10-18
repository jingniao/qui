package collector

import (
	"errors"
	"proxycontrol/pkg/contracts/access"
	"testing"
	"time"
)

// TestMinuteAggregationBucketsAndCounts pins the per-minute contract of the
// access rollup: connections of one minute merge into one counted row, and a
// flush only releases the minutes that already ended.
func TestMinuteAggregationBucketsAndCounts(t *testing.T) {
	aggregator, err := NewAggregator(10)
	if err != nil {
		t.Fatal(err)
	}
	minute := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	record := access.Rollup{NodeID: "node", Revision: 3, InboundProtocol: "socks5", Domain: "example.test", TargetPort: 443, Network: "tcp", RuleID: "rule", Action: "Direct", Allowed: true, Connections: 1}
	first := record
	first.Bucket = minute.Add(15 * time.Second)
	second := record
	second.Bucket = minute.Add(45 * time.Second)
	next := record
	next.Bucket = minute.Add(time.Minute + 5*time.Second)
	for _, entry := range []access.Rollup{first, second, next} {
		if err := aggregator.Add(entry); err != nil {
			t.Fatal(err)
		}
	}
	if aggregator.Len() != 2 {
		t.Fatalf("minutes not separated: %d", aggregator.Len())
	}
	var saved []access.Rollup
	if err := aggregator.Flush(minute.Add(time.Minute), func(records []access.Rollup) error { saved = records; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Connections != 2 || !saved[0].Bucket.Equal(minute) {
		t.Fatalf("minute bucket or count wrong: %+v", saved)
	}
	if aggregator.Len() != 1 {
		t.Fatalf("unfinished minute flushed: %d", aggregator.Len())
	}
	saved = nil
	if err := aggregator.Flush(minute.Add(2*time.Minute), func(records []access.Rollup) error { saved = records; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Connections != 1 || !saved[0].Bucket.Equal(minute.Add(time.Minute)) {
		t.Fatalf("second minute wrong: %+v", saved)
	}
}

func TestMinuteAggregationFlushFailureAndCapacity(t *testing.T) {
	a, err := NewAggregator(1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 10, 0, 15, 0, time.UTC)
	r := access.Rollup{Bucket: now, NodeID: "node", Revision: 1, InboundProtocol: "socks5", Domain: "example.test", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 1}
	for index := 0; index < 50; index++ {
		if err := a.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	other := r
	other.Domain = "other.test"
	if err := a.Add(other); !errors.Is(err, ErrCapacity) {
		t.Fatal("capacity overflow not reported", err)
	}
	if err := a.Flush(now.Add(time.Minute), func(records []access.Rollup) error { return errors.New("disk full") }); err == nil || a.Len() != 1 {
		t.Fatal("failed persistence lost aggregation")
	}
	var saved []access.Rollup
	if err := a.Flush(now.Add(time.Minute), func(records []access.Rollup) error { saved = records; return nil }); err != nil || len(saved) != 1 || saved[0].Connections != 50 || a.Len() != 0 {
		t.Fatal("minute aggregation not persisted", saved, err)
	}
}
