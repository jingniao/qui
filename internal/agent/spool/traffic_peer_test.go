package spool

import (
	"context"

	"proxycontrol/pkg/contracts/traffic"
	"testing"
)

func TestMixedCoreNodeAndAccountEpochsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	q, err := Open(ctx, dir, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	a := sample("xray-epoch", 100)
	b := sample("singbox-epoch", 200)
	for _, c := range []traffic.Counter{a, b} {
		if err := q.SaveTraffic(ctx, []traffic.Counter{c}); err != nil {
			t.Fatal(err)
		}
	}
	n := sample("official-epoch.node", 300)
	n.Kind, n.Core, n.CredentialID, n.UserID, n.DeviceID, n.Protocol = "node", "sing-box", "node", "node", "node", ""
	if err = q.SaveTraffic(ctx, []traffic.Counter{n}); err != nil {
		t.Fatal(err)
	}
	q.Close()
	q, err = Open(ctx, dir, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	// Duplicate cumulative readings from either core do not produce new batches.
	for _, c := range []traffic.Counter{a, b, n} {
		if err = q.SaveTraffic(ctx, []traffic.Counter{c}); err != nil {
			t.Fatal(err)
		}
	}
	var userTotal, nodeTotal int64
	for k := 0; k < 2; k++ {
		batch, _, err := q.PendingTrafficForPeer(ctx, false)
		if err != nil || batch == nil {
			t.Fatal(err)
		}
		for _, c := range batch.Counters {
			if c.Kind != "" {
				t.Fatal("node counter sent to old peer")
			}
			userTotal += c.UploadBytes
		}
		if err = q.AckTraffic(ctx, batch.ID); err != nil {
			t.Fatal(err)
		}
	}
	if batch, _, err := q.PendingTrafficForPeer(ctx, false); err != nil || batch != nil {
		t.Fatal("old peer saw node total", err)
	}
	batch, _, err := q.PendingTrafficForPeer(ctx, true)
	if err != nil || batch == nil {
		t.Fatal("negotiated node batch lost", err)
	}
	nodeTotal = batch.Counters[0].UploadBytes
	if userTotal != 300 || nodeTotal != 300 {
		t.Fatal(userTotal, nodeTotal)
	}
	if err = q.AckTraffic(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	// A restarted core gets its own epoch and can start below the old total.
	n.CoreEpoch = "official-restart.node"
	n.UploadBytes = 10
	if err = q.SaveTraffic(ctx, []traffic.Counter{n}); err != nil {
		t.Fatal(err)
	}
	n.UploadBytes = 9
	if err = q.SaveTraffic(ctx, []traffic.Counter{n}); err == nil {
		t.Fatal("same-epoch decrease accepted")
	}
}
