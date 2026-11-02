package core

import (
	"context"
	"errors"
	"math"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
)

// recordingSink keeps every batch the helper persists, so a test can see the
// account batch and the independent node batch in order.
type recordingSink struct {
	batches [][]traffic.Counter
	failAt  int
}

func (s *recordingSink) save(_ context.Context, counters []traffic.Counter) error {
	s.batches = append(s.batches, append([]traffic.Counter(nil), counters...))
	if s.failAt > 0 && len(s.batches) == s.failAt {
		return errors.New("sink unavailable")
	}
	return nil
}

func TestNodeTotalAddsNodeOnlyBytes(t *testing.T) {
	counters := []traffic.Counter{{UploadBytes: 10, DownloadBytes: 20}, {UploadBytes: 5, DownloadBytes: 1}}
	total, err := NodeTotal("xray", "epoch", 4, counters)
	if err != nil || total.UploadBytes != 15 || total.DownloadBytes != 21 || total.CoreEpoch != "epoch.node" {
		t.Fatal(total, err)
	}
	total, err = NodeTotal("xray", "epoch", 4, counters, StatsBatch{Upload: 7, Download: 9})
	if err != nil || total.UploadBytes != 22 || total.DownloadBytes != 30 {
		t.Fatal("node-only bytes were not added", total, err)
	}
	if _, err := NodeTotal("xray", "epoch", 4, nil, StatsBatch{Upload: math.MaxInt64, Download: 0}, StatsBatch{Upload: 1}); err == nil {
		t.Fatal("overflowing node-only bytes accepted")
	}
}

func TestSaveAccountAndNodePersistsAccountFirstThenNode(t *testing.T) {
	sink := &recordingSink{}
	counters := []traffic.Counter{{UploadBytes: 10, DownloadBytes: 20}}
	if err := SaveAccountAndNode(context.Background(), sink.save, "xray", "epoch", 3, counters, StatsBatch{Upload: 5, Download: 25}); err != nil {
		t.Fatal(err)
	}
	if len(sink.batches) != 2 || len(sink.batches[0]) != 1 || sink.batches[0][0].UploadBytes != 10 {
		t.Fatal("account batch was not persisted first", sink.batches)
	}
	node := sink.batches[1]
	if len(node) != 1 || node[0].Kind != "node" || node[0].Core != "xray" || node[0].CoreEpoch != "epoch.node" || node[0].UploadBytes != 15 || node[0].DownloadBytes != 45 {
		t.Fatal("node batch did not carry the complete total", node)
	}
	if len(node[0].CredentialID) == 0 || node[0].CredentialID != node[0].UserID || node[0].UserID != node[0].DeviceID {
		t.Fatal("node batch invented an account", node[0])
	}
}

func TestSaveAccountAndNodeOverflowWritesNothing(t *testing.T) {
	for name, counters := range map[string][]traffic.Counter{
		"accounts": {{UploadBytes: math.MaxInt64, DownloadBytes: 0}, {UploadBytes: 1}},
		"negative": {{UploadBytes: -1}},
	} {
		sink := &recordingSink{}
		if err := SaveAccountAndNode(context.Background(), sink.save, "xray", "epoch", 3, counters); err == nil || len(sink.batches) != 0 {
			t.Fatal(name, "invalid account bytes reached the sink", sink.batches)
		}
	}
	sink := &recordingSink{}
	if err := SaveAccountAndNode(context.Background(), sink.save, "xray", "epoch", 3, nil, StatsBatch{Upload: math.MaxInt64, Download: math.MaxInt64}, StatsBatch{Download: 1}); err == nil || len(sink.batches) != 0 {
		t.Fatal("overflowing node-only bytes reached the sink", sink.batches)
	}
}

func TestSaveAccountAndNodeRetriesFailedNodeBatch(t *testing.T) {
	sink := &recordingSink{failAt: 2}
	counters := []traffic.Counter{{UploadBytes: 10, DownloadBytes: 20}}
	if err := SaveAccountAndNode(context.Background(), sink.save, "xray", "epoch", 3, counters, StatsBatch{Upload: 5}); err == nil {
		t.Fatal("failed node batch reported success")
	}
	sink.failAt = 0
	if err := SaveAccountAndNode(context.Background(), sink.save, "xray", "epoch", 3, counters, StatsBatch{Upload: 5}); err != nil {
		t.Fatal(err)
	}
	// The retry re-sends the same cumulative sample; panel cursors make it
	// idempotent and no native counter was advanced by the failure.
	last := sink.batches[len(sink.batches)-1]
	if len(last) != 1 || last[0].UploadBytes != 15 || last[0].DownloadBytes != 20 {
		t.Fatal(last)
	}
}
