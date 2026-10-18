package spool

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/traffic"
	"sync"
	"testing"
)

func boundFixture(t *testing.T) (*Spool, string, string) {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "spool")
	witness := filepath.Join(root, "identity", "spool-store.json.enc")
	s, err := OpenBound(context.Background(), directory, "node", 0, witness, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, directory, witness
}
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestBoundStoreLossAndReplacementCannotInitializeAnEmptyLedger(t *testing.T) {
	for _, fault := range []string{"database", "key", "witness", "metadata", "counter-cursors"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			s, directory, witness := boundFixture(t)
			if err := s.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 300)}); err != nil {
				t.Fatal(err)
			}
			original := mustRead(t, witness)
			switch fault {
			case "metadata":
				_, err := s.database.Exec("DELETE FROM spool_store")
				if err != nil {
					t.Fatal(err)
				}
			case "counter-cursors":
				_, err := s.database.Exec("DELETE FROM traffic_cursors")
				if err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			target := ""
			if fault == "database" {
				target = filepath.Join(directory, "queue.db")
			}
			if fault == "key" {
				target = filepath.Join(directory, "spool.key")
			}
			if fault == "witness" {
				target = witness
			}
			if target != "" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			other, err := OpenBound(ctx, directory, "node", 0, witness, nil)
			if err == nil {
				other.Close()
				t.Fatal("inconsistent store accepted", fault)
			}
			if target != "" {
				if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing accounting file silently replaced", fault, err)
				}
			}
			if fault != "witness" && !bytes.Equal(original, mustRead(t, witness)) {
				t.Fatal("failed open changed original witness")
			}
		})
	}
}
func TestBoundStoreOlderDatabaseRejectedAndMatchingStoreCanRecover(t *testing.T) {
	ctx := context.Background()
	s, directory, witness := boundFixture(t)
	oldPath := filepath.Join(t.TempDir(), "old.db")
	if _, err := s.database.Exec("VACUUM INTO ?", oldPath); err != nil {
		t.Fatal(err)
	}
	oldDatabase := mustRead(t, oldPath)
	if err := s.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 300)}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	current := mustRead(t, filepath.Join(directory, "queue.db"))
	currentWitness := mustRead(t, witness)
	if err := os.WriteFile(filepath.Join(directory, "queue.db"), oldDatabase, 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenBound(ctx, directory, "node", 0, witness, nil); err == nil {
		reopened.Close()
		t.Fatal("older database accepted")
	}
	if !bytes.Equal(currentWitness, mustRead(t, witness)) {
		t.Fatal("mismatched restore rewrote current checkpoint")
	}
	if err := os.WriteFile(filepath.Join(directory, "queue.db"), current, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenBound(ctx, directory, "node", 0, witness, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
}
func TestBoundStoreWitnessAheadOfDatabaseAndTamperRemainClosed(t *testing.T) {
	for _, fault := range []string{"ahead", "tamper"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			s, directory, witness := boundFixture(t)
			if fault == "ahead" {
				tx, err := s.database.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				head := s.bound.head
				head.Generation++
				if err := s.writeHead(ctx, tx, head); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
			} else {
				data := mustRead(t, witness)
				data[len(data)-1] ^= 1
				if err := os.WriteFile(witness, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 300)}); !errors.Is(err, ErrStoreIntegrity) {
				t.Fatal("unconfirmed checkpoint still accepted traffic", err)
			}
			s.Close()
			if reopened, err := OpenBound(ctx, directory, "node", 0, witness, nil); err == nil {
				reopened.Close()
				t.Fatal("interrupted checkpoint resumed")
			}
		})
	}
}
func TestMultipleStoreHandlesSerializeAccountingCheckpoint(t *testing.T) {
	ctx := context.Background()
	s, directory, _ := boundFixture(t)
	other, err := Open(ctx, directory, "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wait sync.WaitGroup
	results := make(chan error, 12)
	for index := 0; index < 6; index++ {
		wait.Add(2)
		go func() { defer wait.Done(); results <- s.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 300)}) }()
		go func() { defer wait.Done(); results <- other.SaveTraffic(ctx, []traffic.Counter{sample("epoch", 300)}) }()
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
}
