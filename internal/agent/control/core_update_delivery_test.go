package control

import (
	"context"
	"encoding/json"
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
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

// coreResultPayload is what the worker hands to the session for one finished
// update. The session only forwards it, so a fixture result is enough.
func coreResultPayload(t *testing.T, taskID string) *pb.CoreUpdateResult {
	t.Helper()
	data, err := json.Marshal(release.UpdateResult{ID: taskID, NodeID: "node-update", Revision: 1, Status: "success", Code: "success"})
	if err != nil {
		t.Fatal(err)
	}
	return &pb.CoreUpdateResult{ResultJson: data}
}

// coreResultSession drives one control session for a worker that may have a
// result to deliver and returns the fake panel it talked to plus the error that
// ended it. sendErr breaks the Agent's Send, streamErr is what the panel answers
// the result with instead of an Ack, and a healthy session simply runs until the
// timeout.
func coreResultSession(t *testing.T, manager *reconcile.Reconciler, adapter *fakeCoreRuntime, worker *coreUpdates, sendErr, streamErr error, timeout time.Duration) (*controlStream, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stream := newControlStream(ctx)
	stream.coreResultSendError = sendErr
	stream.coreResultFailure = streamErr
	directory := t.TempDir()
	state := &identity.State{NodeID: "node-update", Directory: directory}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan error, 1)
	go func() {
		done <- session(ctx, controlClient{stream: stream}, state, logger, capability.NewSampler(directory), worker.capabilities, "test", manager, adapter, nil, worker, nil)
	}()
	select {
	case err := <-done:
		return stream, err
	case <-ctx.Done():
		<-done
		return stream, ctx.Err()
	}
}

// pendingCoreResults counts the results the worker still has to deliver.
func pendingCoreResults(worker *coreUpdates) int {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return len(worker.pending)
}

// A result whose delivery failed must survive the session. Before the pending
// list existed the result lived in a session-local slice, so the next session
// found an empty channel and the panel never learned the outcome.
func TestSessionResendsCoreUpdateResultAfterSendFailure(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, _, manager, _ := updateHarness(t, adapter)
	worker.results <- coreResultPayload(t, "upd_send_failed")

	first, err := coreResultSession(t, manager, adapter, worker, status.Error(codes.Unavailable, "stream reset"), nil, 3*time.Second)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("first session did not end on the send failure: %v", err)
	}
	if attempts := first.coreUpdateResults(); len(attempts) != 1 {
		t.Fatalf("first session sent %d results", len(attempts))
	}
	if pending := pendingCoreResults(worker); pending != 1 {
		t.Fatalf("the failed result was forgotten: %d pending", pending)
	}

	second, err := coreResultSession(t, manager, adapter, worker, nil, nil, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second session ended unexpectedly: %v", err)
	}
	if attempts := second.coreUpdateResults(); len(attempts) != 1 {
		t.Fatalf("second session sent %d results, want the same one again", len(attempts))
	}
	if pending := pendingCoreResults(worker); pending != 0 {
		t.Fatalf("the acknowledged result is still pending: %d", pending)
	}
}

// A panel that stored the outcome but lost the stream before its Ack is the
// other half of the same failure: the result has to be sent again, and then not
// at all, because the panel acknowledges an identical duplicate.
func TestSessionResendsCoreUpdateResultAfterLostAck(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, _, manager, _ := updateHarness(t, adapter)
	worker.results <- coreResultPayload(t, "upd_lost_ack")

	first, err := coreResultSession(t, manager, adapter, worker, nil, status.Error(codes.Unavailable, "connection reset by peer"), 3*time.Second)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("first session did not end on the lost stream: %v", err)
	}
	if attempts := first.coreUpdateResults(); len(attempts) != 1 {
		t.Fatalf("first session sent %d results", len(attempts))
	}
	if pending := pendingCoreResults(worker); pending != 1 {
		t.Fatalf("the unacknowledged result was forgotten: %d pending", pending)
	}

	second, err := coreResultSession(t, manager, adapter, worker, nil, nil, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second session ended unexpectedly: %v", err)
	}
	if attempts := second.coreUpdateResults(); len(attempts) != 1 {
		t.Fatalf("second session sent %d results, want exactly one", len(attempts))
	}
	third, err := coreResultSession(t, manager, adapter, worker, nil, nil, 500*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third session ended unexpectedly: %v", err)
	}
	if attempts := third.coreUpdateResults(); len(attempts) != 0 {
		t.Fatalf("an acknowledged result was sent again %d times", len(attempts))
	}
}

// The panel refuses a receipt for good when the task is gone, and it says so by
// ending the stream with FailedPrecondition. Retrying that forever would keep
// the node reconnecting without ever succeeding, so the result is dropped after
// the limit.
func TestSessionDropsRepeatedlyRejectedCoreUpdateResult(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, _, manager, _ := updateHarness(t, adapter)
	worker.results <- coreResultPayload(t, "upd_rejected")
	rejection := status.Error(codes.FailedPrecondition, "core update result rejected")

	for attempt := 1; attempt <= coreUpdateRejectLimit; attempt++ {
		stream, err := coreResultSession(t, manager, adapter, worker, nil, rejection, 3*time.Second)
		if !coreUpdateResultRejected(err) {
			t.Fatalf("session %d did not end on the panel rejection: %v", attempt, err)
		}
		if sent := stream.coreUpdateResults(); len(sent) != 1 {
			t.Fatalf("session %d sent %d results", attempt, len(sent))
		}
		want := 1
		if attempt == coreUpdateRejectLimit {
			want = 0
		}
		if pending := pendingCoreResults(worker); pending != want {
			t.Fatalf("after rejection %d: %d pending, want %d", attempt, pending, want)
		}
	}

	// Nothing is left to send, so a healthy session stays quiet.
	stream, err := coreResultSession(t, manager, adapter, worker, nil, nil, 500*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("session after the drop ended unexpectedly: %v", err)
	}
	if sent := stream.coreUpdateResults(); len(sent) != 0 {
		t.Fatalf("the dropped result was sent again %d times", len(sent))
	}
}

// Only the panel's own permanent rejection may count against a result. A
// transport failure, a cancellation or any other status keeps it forever.
func TestCoreUpdateResultRejectionCountingIgnoresOtherErrors(t *testing.T) {
	if !coreUpdateResultRejected(status.Error(codes.FailedPrecondition, "core update result rejected")) {
		t.Fatal("the panel's rejection is not recognised")
	}
	for _, err := range []error{
		io.EOF,
		context.Canceled,
		errors.New("local failure"),
		status.Error(codes.Unavailable, "core update result rejected"),
		status.Error(codes.FailedPrecondition, "core updates unavailable"),
		status.Error(codes.FailedPrecondition, "deployment unavailable"),
	} {
		if coreUpdateResultRejected(err) {
			t.Fatalf("%v counted as the panel's permanent rejection", err)
		}
	}
}

// Other failures never drop the result, however often they repeat.
func TestSessionKeepsCoreUpdateResultAfterOtherFailures(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27"}
	worker, _, manager, _ := updateHarness(t, adapter)
	worker.results <- coreResultPayload(t, "upd_transient")
	for attempt := 1; attempt <= coreUpdateRejectLimit+1; attempt++ {
		_, err := coreResultSession(t, manager, adapter, worker, nil, status.Error(codes.Unavailable, "control channel lost"), 3*time.Second)
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("session %d did not end on the transport failure: %v", attempt, err)
		}
	}
	if pending := pendingCoreResults(worker); pending != 1 {
		t.Fatalf("a transport failure dropped the result: %d pending", pending)
	}
	worker.mu.Lock()
	rejections := worker.rejections
	worker.mu.Unlock()
	if rejections != 0 {
		t.Fatalf("transport failures counted as %d rejections", rejections)
	}
}
