package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"encoding/json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"proxycontrol/internal/agent/capability"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// Only the panel's deterministic telemetry rejection may count against a batch.
// Transport failures, cancellation and identity errors stay retryable.
func TestTrafficRejectedOnlyCountsPanelRejections(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	err, _ := runSessionStream(t, cfg, manager, adapter, queue, timeout, func(stream *controlStream) { stream.rejectTraffic = reject })
	return err
}

// runSessionStream is runSession with the fake panel configured by the caller;
// it also returns the stream so the test can read what the Agent sent.
func runSessionStream(t *testing.T, cfg config.Agent, manager *reconcile.Reconciler, adapter *fakeCoreRuntime, queue *spool.Spool, timeout time.Duration, configure func(*controlStream)) (error, *controlStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stream := newControlStream(ctx)
	configure(stream)
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
		return err, stream
	case <-ctx.Done():
		<-done
		return ctx.Err(), stream
	}
}

// The panel rejects one access batch for good. The session must not reconnect
// forever on it: the third rejection quarantines it and the next session
// delivers the later access batch.
func TestSessionQuarantinesARepeatedlyRejectedAccessBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, _ := updateHarness(t, adapter)
	queue, err := spool.Open(ctx, t.TempDir(), "node-update", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	record := func(domain string) []access.Rollup {
		return []access.Rollup{{Bucket: time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC), NodeID: "node-update", Revision: 1, InboundProtocol: "vless", Domain: domain, TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 1}}
	}
	first, err := queue.Enqueue(ctx, record("rejected.example"))
	if err != nil || len(first.BatchIDs) != 1 {
		t.Fatal(first, err)
	}
	if _, err := queue.Enqueue(ctx, record("later.example")); err != nil {
		t.Fatal(err)
	}
	rejected := first.BatchIDs[0]
	for attempt := 1; attempt <= spool.TrafficRejectLimit; attempt++ {
		failed, _ := runSessionStream(t, cfg, manager, adapter, queue, 5*time.Second, func(s *controlStream) { s.rejectAccess = rejected })
		if !trafficRejected(failed) {
			t.Fatalf("session %d did not end on the panel rejection: %v", attempt, failed)
		}
	}
	if head, err := queue.Pending(ctx); err != nil || head == nil || head.ID == rejected {
		t.Fatalf("rejected access batch still blocks the queue: %+v %v", head, err)
	}
	// The next session delivers the later batch and keeps running (until the
	// timeout) instead of ending on a rejection.
	failed, _ := runSessionStream(t, cfg, manager, adapter, queue, 2*time.Second, func(s *controlStream) { s.rejectAccess = rejected })
	if trafficRejected(failed) {
		t.Fatalf("the session still ended on the rejected batch: %v", failed)
	}
	if pending, err := queue.Pending(ctx); err != nil || pending != nil {
		t.Fatalf("later access batch was not delivered or the quarantined one returned early: %+v %v", pending, err)
	}
	if stats, err := queue.Stats(ctx); err != nil || stats.Batches != 1 {
		t.Fatalf("quarantined access batch was not retained: %+v %v", stats, err)
	}
}

// v1.5.3 ordering: an apply that ran while an earlier message awaited its
// acknowledgment has already switched the cores, and queued telemetry carries the
// new revision. The panel refuses telemetry for a revision it has not seen
// confirmed, so the ApplyResult must go out before that traffic batch.
func TestSessionSendsApplyResultBeforeTrafficOfTheNewRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	_, cfg, manager, _ := updateHarness(t, adapter)
	queue, err := spool.Open(ctx, t.TempDir(), "node-update", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.SaveTraffic(ctx, []traffic.Counter{{CoreEpoch: "epoch-new", Revision: 2, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "http", UploadBytes: 10, DownloadBytes: 20, ObservedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	next := desiredstate.State{SchemaVersion: 1, NodeID: "node-update", Revision: 2, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: "user", Enabled: true}}, Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "http", Username: "update-user", Secret: "update-device-password"}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: freeUpdatePort(t), Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	data, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	_, stream := runSessionStream(t, cfg, manager, adapter, queue, 2*time.Second, func(s *controlStream) {
		s.applyBeforeAck = &pb.ApplyCommand{DesiredStateJson: data}
	})
	stream.mu.Lock()
	defer stream.mu.Unlock()
	resultAt, trafficAt := -1, -1
	for index, message := range stream.sent {
		if message.GetApplyResult() != nil && resultAt < 0 {
			resultAt = index
		}
		if message.GetTrafficBatch() != nil && trafficAt < 0 {
			trafficAt = index
		}
	}
	if resultAt < 0 || trafficAt < 0 {
		t.Fatalf("apply result or traffic batch never sent: result=%d traffic=%d", resultAt, trafficAt)
	}
	if resultAt > trafficAt {
		t.Fatalf("traffic batch of the new revision (message %d) was sent before the apply result (message %d)", trafficAt, resultAt)
	}
	if result := stream.sent[resultAt].GetApplyResult(); result.Revision != 2 || !result.Success {
		t.Fatalf("unexpected apply result: %+v", result)
	}
}
