package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/tuning"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestTuningWaitsForMatchingStatusAndRejectsBeforePublish(t *testing.T) {
	// Not parallel: it holds the package-wide tuningSlot.
	dir := t.TempDir()
	proc := t.TempDir()
	helper := filepath.Join(dir, "usr/local/bin/proxy-agent")
	if err := os.MkdirAll(filepath.Dir(helper), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("bootstrap"), 0700); err != nil {
		t.Fatal(err)
	}
	command := &pb.TuningCommand{RequestId: "new", Settings: map[string]string{"net.ipv4.tcp_ecn": "2"}}
	stale, _ := json.Marshal(&pb.TuningResult{RequestId: "old", Status: "applied"})
	if err := os.WriteFile(filepath.Join(dir, "status.json"), stale, 0600); err != nil {
		t.Fatal(err)
	}
	if missing := runTuning(context.Background(), command, dir, proc, dir, true); missing.Status != "failed" || len(missing.Keys) == 0 || !strings.Contains(missing.Keys[0].Error, "probe.json") {
		t.Fatal("missing probe accepted", missing)
	}
	if err := os.MkdirAll(filepath.Join(proc, "kernel/random"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "kernel/random/boot_id"), []byte("boot-a"), 0600); err != nil {
		t.Fatal(err)
	}
	keys := []*pb.TuningKeyState{}
	for _, key := range tuning.Keys() {
		keys = append(keys, &pb.TuningKeyState{Key: key, Present: true, Writable: true})
	}
	probe, _ := json.Marshal(map[string]any{"boot_id": "boot-a", "keys": keys, "helper_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("bootstrap")))})
	if err := os.WriteFile(filepath.Join(dir, "probe.json"), probe, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan *pb.TuningResult, 1)
	go func() { done <- runTuning(ctx, command, dir, proc, dir, true) }()
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
		got := runTuning(ctx, &pb.TuningCommand{RequestId: "bad", Settings: map[string]string{"kernel.core_pattern": "evil"}}, invalidDir, proc, dir, supported)
		if got.Status != "failed" {
			t.Fatal(got)
		}
	}
	if _, err := os.Stat(filepath.Join(invalidDir, "desired.json")); !os.IsNotExist(err) {
		t.Fatal("published invalid request", err)
	}
	expired, stop := context.WithCancel(context.Background())
	stop()
	if got := runTuning(expired, &pb.TuningCommand{RequestId: "timeout"}, dir, proc, dir, true); got.Status != "failed" {
		t.Fatal(got)
	}
}

func TestTuningUnconfirmedReportsReceiptReadFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := runTuning(ctx, &pb.TuningCommand{RequestId: "receipt-check", Clear: true}, dir, t.TempDir(), dir, true)
	if len(result.Keys) != 1 || !strings.Contains(result.Keys[0].Error, "unexpected end of JSON input") || !strings.Contains(result.Keys[0].Error, "unconfirmed") {
		t.Fatal(result)
	}
}

func TestNewTuningWaitsForPreviousModuleTriggers(t *testing.T) {
	for _, install := range []bool{false, true} {
		t.Run(fmt.Sprintf("install=%t", install), func(t *testing.T) {
			host := t.TempDir()
			proc := filepath.Join(host, "proc/sys")
			dir := filepath.Join(host, "var/lib/proxy-agent/tuning")
			status := filepath.Join(host, "var/lib/qui-tuning-modules/status.json")
			write := func(path, data string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(proc, "kernel/random/boot_id"), "boot")
			write(filepath.Join(proc, "kernel/osrelease"), "6.8.0-100-generic")
			write(filepath.Join(proc, "net/core/default_qdisc"), "fq")
			write(filepath.Join(host, "usr/local/bin/proxy-agent"), "helper")
			var keys []*pb.TuningKeyState
			for _, key := range tuning.Keys() {
				keys = append(keys, &pb.TuningKeyState{Key: key, Present: true, Writable: true})
			}
			probe, _ := json.Marshal(map[string]any{"boot_id": "boot", "module_helper": true, "helper_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("helper"))), "keys": keys})
			write(filepath.Join(dir, "probe.json"), string(probe))
			write(status, `{"id":"previous","boot":"boot","release":"6.8.0-100-generic","targets":["fq"],"finished":false}`)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			result := runTuning(ctx, &pb.TuningCommand{RequestId: "next", Settings: map[string]string{"net.core.default_qdisc": "fq"}, InstallMissingModules: install}, dir, proc, host, true)
			if _, err := os.Stat(filepath.Join(dir, "desired.json")); !os.IsNotExist(err) {
				t.Fatal("started sysctl while the previous dpkg triggers were unfinished")
			}
			if !strings.Contains(result.GetKeys()[0].GetError(), "previous") {
				t.Fatal(result)
			}
		})
	}
}
