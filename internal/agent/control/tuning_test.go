package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestTuningWaitsForMatchingStatusAndRejectsBeforePublish(t *testing.T) {
	dir := t.TempDir()
	proc := t.TempDir()
	command := &pb.TuningCommand{RequestId: "new", Settings: map[string]string{"net.ipv4.tcp_ecn": "2"}}
	stale, _ := json.Marshal(&pb.TuningResult{RequestId: "old", Status: "applied"})
	if err := os.WriteFile(filepath.Join(dir, "status.json"), stale, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan *pb.TuningResult, 1)
	go func() { done <- runTuning(ctx, command, dir, proc, true) }()
	deadline := time.After(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "desired.json")); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no desired request")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-done:
		t.Fatal("accepted stale result")
	default:
	}
	result := &pb.TuningResult{RequestId: "new", Status: "applied", Keys: []*pb.TuningKeyResult{{Key: "net.ipv4.tcp_ecn", Value: "2", Applied: true}}}
	raw, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, "status.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.Status != "applied" || got.RequestId != "new" {
		t.Fatal(got)
	}
	invalidDir := t.TempDir()
	for _, supported := range []bool{false, true} {
		got := runTuning(ctx, &pb.TuningCommand{RequestId: "bad", Settings: map[string]string{"kernel.core_pattern": "evil"}}, invalidDir, proc, supported)
		if got.Status != "failed" {
			t.Fatal(got)
		}
	}
	if _, err := os.Stat(filepath.Join(invalidDir, "desired.json")); !os.IsNotExist(err) {
		t.Fatal("published invalid request", err)
	}
	expired, stop := context.WithCancel(context.Background())
	stop()
	if got := runTuning(expired, &pb.TuningCommand{RequestId: "timeout"}, dir, proc, true); got.Status != "failed" {
		t.Fatal(got)
	}
}
