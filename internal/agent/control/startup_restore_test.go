package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/spool"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// A startup restore that could not start the retained revision must keep the
// worker alive, while every other failure stays fatal.
func TestStartupRestoreNonFatalSentinels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"release recovery", core.ErrReleaseRecovery, true},
		{"release recovery wrapped", fmt.Errorf("repair: %w", core.ErrReleaseRecovery), true},
		{"stop unconfirmed", core.ErrCoreStopUnconfirmed, true},
		{"stop unconfirmed joined", errors.Join(errors.New("stop"), core.ErrCoreStopUnconfirmed), true},
		{"runtime restore wrapped", fmt.Errorf("%w: %w", reconcile.ErrRuntimeRestore, errors.New("render failed")), true},
		{"runtime restore joined", errors.Join(errors.New("render"), reconcile.ErrRuntimeRestore), true},
		{"integrity", errors.New("current revision checksum mismatch"), false},
		{"identity", errors.New("saved revision identity mismatch"), false},
		{"nil", nil, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := startupRestoreNonFatal(test.err); got != test.want {
				t.Fatalf("startupRestoreNonFatal(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestQueueOpenFailureBlocksOrdinaryStartup(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreRuntime{name: "xray"}
	manager, err := reconcile.New(directory, "node-startup", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, startupRevision("xray", 1)); err != nil || !result.Success {
		t.Fatalf("seed retained revision: %+v %v", result, err)
	}
	restartedAdapter := &fakeCoreRuntime{name: "xray"}
	restarted, err := reconcile.New(directory, "node-startup", restartedAdapter)
	if err != nil {
		t.Fatal(err)
	}
	queueDir := filepath.Join(directory, "spool")
	if err := os.WriteFile(queueDir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	queue, queueErr := spool.OpenBound(ctx, queueDir, "node-startup", 0, filepath.Join(directory, "spool-store.json.enc"), nil)
	if queueErr == nil {
		t.Fatal("queue opened on a regular file")
	}
	var repaired bool
	err = restoreOrdinaryStartup(ctx, restarted, queueErr, func() error {
		repaired = true
		return nil
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("queue error prevented control startup: %v", err)
	}
	if repaired || len(restartedAdapter.applied) != 0 {
		t.Errorf("startup prepared or applied a core without accounting: repair=%t applies=%d", repaired, len(restartedAdapter.applied))
	}
	if !errors.Is(restarted.StartupError(), queueErr) {
		t.Errorf("queue error not preserved: %v", restarted.StartupError())
	}
	if healthy, err := managedStartupHealth(ctx, restarted, false, true); err != nil || healthy {
		t.Errorf("managed readiness reported healthy: %t %v", healthy, err)
	}
	if got := sampleSpoolStatus(ctx, queue); got.GetFailureCode() != "queue_unavailable" {
		t.Errorf("queue failure not visible to the panel: %v", got)
	}
	heartbeat := &pb.Heartbeat{CurrentRevision: restarted.Current()}
	reportCoreHealth(ctx, heartbeat, restarted, "xray")
	if heartbeat.CoreHealthy == nil || heartbeat.GetCoreHealthy() || heartbeat.GetCoreError() != "core_unhealthy" {
		t.Errorf("queue failure not reflected in node health: %v", heartbeat)
	}
	if result, err := restarted.Apply(ctx, startupRevision("xray", 2)); !errors.Is(err, core.ErrReleaseRecovery) || result.Success {
		t.Errorf("publication started a core without accounting: %+v %v", result, err)
	}
}

func TestRuntimeRestoreFailureBlocksManagedReadinessUntilPublication(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manager, err := reconcile.New(directory, "node-startup", &fakeCoreRuntime{name: "sing-box"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, startupRevision("sing-box", 1)); err != nil || !result.Success {
		t.Fatalf("seed retained revision: %+v %v", result, err)
	}
	runtime, err := core.NewMultiRuntime(&fakeCoreRuntime{name: "xray"})
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reconcile.New(directory, "node-startup", runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(ctx); !errors.Is(err, reconcile.ErrRuntimeRestore) {
		t.Fatalf("expected initial runtime restore error: %v", err)
	}
	if healthy, err := managedStartupHealth(ctx, restarted, false, false); healthy || !errors.Is(err, reconcile.ErrRuntimeRestore) {
		t.Errorf("normal readiness accepted failed restore: %t %v", healthy, err)
	}
	if healthy, err := managedStartupHealth(ctx, restarted, false, true); healthy || err != nil {
		t.Errorf("degraded readiness not reported: %t %v", healthy, err)
	}
	heartbeat := &pb.Heartbeat{CurrentRevision: restarted.Current()}
	reportCoreHealth(ctx, heartbeat, restarted, "xray")
	if heartbeat.CoreHealthy == nil || heartbeat.GetCoreHealthy() || heartbeat.GetCoreError() != "core_unhealthy" ||
		len(heartbeat.Cores) != 1 || heartbeat.Cores[0].GetHealthy() || heartbeat.Cores[0].GetError() != "core_unhealthy" {
		t.Errorf("restore failure not reflected in node and core health: %v", heartbeat)
	}
	if result, err := restarted.Apply(ctx, startupRevision("xray", 2)); err != nil || !result.Success {
		t.Fatalf("publish compatible revision: %+v %v", result, err)
	}
	if healthy, err := managedStartupHealth(ctx, restarted, false, false); !healthy || err != nil {
		t.Fatalf("compatible revision did not restore health: %t %v", healthy, err)
	}
}

func startupRevision(name string, revision int64) desiredstate.State {
	return desiredstate.State{
		SchemaVersion: 1, Revision: revision, NodeID: "node-startup",
		Logging:   desiredstate.Logging{DomainOnly: true},
		Inbounds:  []desiredstate.Inbound{{ID: "in", Core: name, Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, Enabled: true, DefaultOutbound: "direct"}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
}
