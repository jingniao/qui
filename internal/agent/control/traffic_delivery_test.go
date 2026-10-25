package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"proxycontrol/internal/agent/capability"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

// Only the panel's deterministic telemetry rejection may count against a batch.
// Transport failures, cancellation and identity errors stay retryable.
func TestTrafficRejectedOnlyCountsPanelRejections(t *testing.T) {
	if !trafficRejected(status.Error(codes.FailedPrecondition, "traffic batch rejected")) {
		t.Fatal("panel rejection not recognised")
	}
	if !trafficRejected(status.Error(codes.InvalidArgument, "invalid traffic batch")) {
		t.Fatal("invalid batch not recognised")
	}
	for _, err := range []error{io.EOF, context.Canceled, errors.New("local failure"), status.Error(codes.Unavailable, "connection reset"), status.Error(codes.Unauthenticated, "certificate revoked")} {
		if trafficRejected(err) {
			t.Fatalf("%v counted as a deterministic rejection", err)
		}
	}
}

// The whole defence against one unrepairable batch: the panel's rejection of the
// head is counted, and after the limit the batch leaves the head so later traffic
// is delivered again while the rejected records stay on disk.
func TestSessionQuarantinesARepeatedlyRejectedTrafficBatch(t *testing.T) {
	ctx := context.Background()
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, _ := updateHarness(t, adapter)
	queue, err := spool.Open(ctx, t.TempDir(), "node-update", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.SaveTraffic(ctx, []traffic.Counter{{CoreEpoch: "epoch-rejected", Revision: 1, CredentialID: "rejected", UserID: "user", DeviceID: "device", Protocol: "vless", UploadBytes: 10, DownloadBytes: 20, ObservedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{{CoreEpoch: "epoch-later", Revision: 1, CredentialID: "later", UserID: "user", DeviceID: "device", Protocol: "vless", UploadBytes: 30, DownloadBytes: 40, ObservedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	rejected, err := queue.PendingTraffic(ctx)
	if err != nil || rejected == nil {
		t.Fatalf("no head batch: %+v %v", rejected, err)
	}
	for attempt := 1; attempt <= spool.TrafficRejectLimit; attempt++ {
		if failed := runSession(t, cfg, manager, adapter, queue, rejected.ID, 5*time.Second); !trafficRejected(failed) {
			t.Fatalf("session %d did not end on the panel rejection: %v", attempt, failed)
		}
	}
	stats, err := queue.Stats(ctx)
	if err != nil || stats.TrafficQuarantined != 1 {
		t.Fatalf("rejected batch not quarantined: %+v %v", stats, err)
	}
	later, err := queue.PendingTraffic(ctx)
	if err != nil || later == nil || later.ID == rejected.ID {
		t.Fatalf("later traffic still blocked: %+v %v", later, err)
	}
	// A healthy session keeps running until it is cancelled, so only the
	// delivery itself is asserted here.
	if failed := runSession(t, cfg, manager, adapter, queue, "", 2*time.Second); trafficRejected(failed) {
		t.Fatalf("the next batch was rejected too: %v", failed)
	}
	if pending, err := queue.PendingTraffic(ctx); err != nil || pending != nil {
		t.Fatalf("accepted traffic was not acknowledged: %+v %v", pending, err)
	}
	if stats, err := queue.Stats(ctx); err != nil || stats.TrafficQuarantined != 1 || stats.Batches != 1 {
		t.Fatalf("quarantined records were deleted: %+v %v", stats, err)
	}
}

// runSession drives one control session until it ends and returns the error that
// ended it; the session is cancelled after the given time so a missing rejection
// cannot hang the test.
func runSession(t *testing.T, cfg config.Agent, manager *reconcile.Reconciler, adapter *fakeCoreRuntime, queue *spool.Spool, reject string, timeout time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stream := newControlStream(ctx)
	stream.rejectTraffic = reject
	directory := t.TempDir()
	state := &identity.State{NodeID: "node-update", Directory: directory}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	holder := newCapabilityState(&pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27"})
	done := make(chan error, 1)
	go func() {
		done <- session(ctx, controlClient{stream: stream}, state, logger, capability.NewSampler(directory), holder, "test", manager, adapter, queue, nil, nil)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		<-done
		return ctx.Err()
	}
}
