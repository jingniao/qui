package server

import (
	"context"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/notification"
)

func TestNotificationMetricsPreserveMinuteGaps(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	ctx := context.Background()
	nodes := &node.Repository{Database: app.Database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "metrics node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Minute).Add(-30 * time.Minute)
	for i := 0; i < 30; i++ {
		if _, err := app.Database.ExecContext(ctx, "INSERT INTO node_metric_samples(node_id,at,cpu_percent,memory_percent) VALUES(?,?,95,95)", n.ID, start.Add(time.Duration(i)*time.Minute).Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	sources := &nodeNotificationSources{nodes: nodes}
	rules := []notification.Rule{{Kind: "cpu", Enabled: true, Threshold: 90, DurationMinutes: 30}}
	check := func(want int) {
		t.Helper()
		metrics, err := sources.Metrics(ctx, start)
		if err != nil {
			t.Fatal(err)
		}
		events, _ := notification.Evaluate(rules, notification.Snapshot{Now: start.Add(30 * time.Minute), Nodes: []notification.NodeSnapshot{{ID: n.ID, Status: "online"}}, Metrics: metrics}, nil)
		if len(events) != want {
			t.Fatalf("got %d events, want %d; samples=%d", len(events), want, len(metrics[n.ID].CPU))
		}
	}
	check(1)
	if _, err := app.Database.ExecContext(ctx, "DELETE FROM node_metric_samples WHERE node_id=? AND at=?", n.ID, start.Add(15*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// Include an older minute: compression would incorrectly assemble 30 samples.
	if _, err := app.Database.ExecContext(ctx, "INSERT INTO node_metric_samples(node_id,at,cpu_percent) VALUES(?,?,95)", n.ID, start.Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	start = start.Add(-time.Minute)
	check(0)
}
