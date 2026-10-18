package spool

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/access"
	"testing"
	"time"
)

func row(domain string, blocked bool) access.Rollup {
	action := "Direct"
	if blocked {
		action = "Block"
	}
	return access.Rollup{Bucket: time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC), NodeID: "node", Revision: 1, InboundProtocol: "vless", Domain: domain, TargetPort: 443, Network: "tcp", Action: action, Allowed: !blocked, Connections: 50}
}

func TestSpoolMissingKeyAndDifferentIdentityDoNotReplaceExistingQueue(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(ctx, []access.Rollup{row("private.example", false)}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if other, err := Open(ctx, directory, "other-node", 0); err == nil {
		other.Close()
		t.Fatal("queue attached to another identity")
	}
	if err := os.Remove(filepath.Join(directory, "spool.key")); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(ctx, directory, "node", 0); err == nil {
		other.Close()
		t.Fatal("missing key replaced")
	}
	if _, err := os.Stat(filepath.Join(directory, "spool.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new key silently generated")
	}
}

func TestSpoolEncryptionRestartAcknowledgementAndTamper(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	s, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Enqueue(ctx, []access.Rollup{row("private.example", false), row("blocked.example", true)})
	if err != nil || len(result.BatchIDs) != 2 {
		t.Fatal(result, err)
	}
	var payload []byte
	if err := s.database.QueryRow("SELECT payload FROM batches LIMIT 1").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("private.example")) {
		t.Fatal("queue stored plaintext domain")
	}
	s.Close()
	s, err = Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pending, err := s.Pending(ctx)
	if err != nil || pending == nil || pending.ID != result.BatchIDs[0] {
		t.Fatal("pending batch lost across restart", err)
	}
	if err := s.Ack(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Ack(ctx, pending.ID); err != nil {
		t.Fatal("duplicate acknowledgement", err)
	}
	pending, err = s.Pending(ctx)
	if err != nil || pending == nil || pending.Records[0].Allowed {
		t.Fatal("blocked batch missing", err)
	}
	if err := s.database.QueryRow("SELECT payload FROM batches WHERE id=?", pending.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 1
	if _, err := s.database.Exec("UPDATE batches SET payload=? WHERE id=?", payload, pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pending(ctx); err == nil {
		t.Fatal("tampered batch accepted")
	}
}

func TestSpoolCapacityDropsOldestAllowedAndPreservesBlocked(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blocked, err := s.Enqueue(ctx, []access.Rollup{row("blocked.example", true)})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Enqueue(ctx, []access.Rollup{row("first.example", false)})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.limit = initial.Bytes
	next, err := s.Enqueue(ctx, []access.Rollup{row("other.example", false)})
	if err != nil || next.DroppedAllowedRollups != 1 {
		t.Fatal("old allowed batch not evicted", next, err)
	}
	var count int
	s.database.QueryRow("SELECT count(*) FROM batches WHERE id=?", blocked.BatchIDs[0]).Scan(&count)
	if count != 1 {
		t.Fatal("blocked batch evicted")
	}
	s.database.QueryRow("SELECT count(*) FROM batches WHERE id=?", first.BatchIDs[0]).Scan(&count)
	if count != 0 {
		t.Fatal("oldest allowed batch retained")
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Bytes > s.limit || stats.DroppedAllowedRollups != 1 {
		t.Fatal(stats, err)
	}
	if _, err := s.Enqueue(ctx, []access.Rollup{row("another-blocked.example", true), row("yet-another-blocked.example", true)}); !errors.Is(err, ErrFull) {
		t.Fatal("overflow not reported", err)
	}
	after, _ := s.Stats(ctx)
	if after != stats {
		t.Fatal("failed enqueue altered existing queue", stats, after)
	}
}
