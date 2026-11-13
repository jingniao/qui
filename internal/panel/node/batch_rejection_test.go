package node

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type codedRejection struct {
	reason   string
	revision int64
}

func (e codedRejection) Error() string                    { return "rejected" }
func (e codedRejection) RejectionReason() (string, int64) { return e.reason, e.revision }

func TestBatchRejectionLogsFixedReasonOncePerMinute(t *testing.T) {
	var output bytes.Buffer
	control := &Control{Logger: slog.New(slog.NewTextHandler(&output, nil))}
	identity := fmt.Errorf("recording: %w", codedRejection{"identity_conflict", 42})
	control.logBatchRejection("node-a", "traffic", "batch-1", identity)
	control.logBatchRejection("node-a", "traffic", "batch-1", identity)
	if got := strings.Count(output.String(), "agent batch rejected"); got != 1 {
		t.Fatalf("logged %d warnings within a minute, want 1:\n%s", got, output.String())
	}
	for _, want := range []string{"level=WARN", "node_id=node-a", "kind=traffic", "reason=identity_conflict", "batch_id=batch-1", "revision=42"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, output.String())
		}
	}
	// Another reason, kind or node is its own stream of warnings. An error
	// without a code is logged as "invalid" and carries no free text.
	control.logBatchRejection("node-a", "traffic", "", codedRejection{"future_time", 0})
	control.logBatchRejection("node-a", "access", "", codedRejection{"identity_conflict", 0})
	control.logBatchRejection("node-b", "traffic", "", codedRejection{"identity_conflict", 0})
	control.logBatchRejection("node-a", "access", "", errors.New("secret.example.com user alice"))
	if got := strings.Count(output.String(), "agent batch rejected"); got != 5 {
		t.Fatalf("logged %d warnings, want 5:\n%s", got, output.String())
	}
	if strings.Contains(output.String(), "alice") || !strings.Contains(output.String(), "reason=invalid") {
		t.Fatalf("uncoded error not reduced to a fixed reason:\n%s", output.String())
	}
}

func TestRejectionLogAllowsAgainAfterInterval(t *testing.T) {
	var log rejectionLog
	start := time.Now()
	if !log.allow("k", start) || log.allow("k", start.Add(rejectionLogInterval-time.Second)) {
		t.Fatal("second warning inside the interval allowed")
	}
	if !log.allow("other", start.Add(rejectionLogInterval)) || !log.allow("k", start.Add(rejectionLogInterval)) {
		t.Fatal("warning after the interval refused")
	}
	log.allow("k2", start.Add(3*rejectionLogInterval))
	if len(log.last) != 1 {
		t.Fatalf("expired keys kept: %v", log.last)
	}
}
