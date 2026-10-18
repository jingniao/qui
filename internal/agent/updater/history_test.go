package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"proxycontrol/internal/platform/filelock"
	"proxycontrol/pkg/contracts/release"
)

func historyFixture(t *testing.T) (HistoryOptions, release.Signed, release.Policy, ed25519.PrivateKey) {
	t.Helper()
	_, signed, policy := stageFixture(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err = release.Sign(signed.Manifest, private)
	if err != nil {
		t.Fatal(err)
	}
	policy.Keys = map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{"proxy-agent", "xray", "sing-box"}}}
	directory := t.TempDir()
	return HistoryOptions{Directory: filepath.Join(directory, "releases", "history"), Checkpoint: filepath.Join(directory, "identity", "release-checkpoint.enc"), NodeID: "node-release-test", Key: bytes.Repeat([]byte{0x42}, 32)}, signed, policy, private
}

func TestAcceptanceHistoryPersistsSequencesConflictsRevocationAndOwnership(t *testing.T) {
	options, signed, policy, private := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenHistory(options); !errors.Is(err, filelock.ErrBusy) {
		if other != nil {
			other.Close()
		}
		t.Fatal("simultaneous history owner", err)
	}
	if err := h.Accept(context.Background(), signed, policy); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- h.Accept(context.Background(), signed, policy) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if h.head.Number != 1 {
		t.Fatal("retry created extra history")
	}
	m := signed.Manifest
	m.Sequence = 2
	next, _ := release.Sign(m, private)
	if err := h.Accept(context.Background(), next, policy); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Check(signed, policy); !errors.Is(err, ErrReplay) {
		t.Fatal("old sequence accepted after restart", err)
	}
	m.Version = "different-same-sequence"
	conflict, _ := release.Sign(m, private)
	if err := h.Check(conflict, policy); !errors.Is(err, ErrReplay) {
		t.Fatal("same sequence identity changed", err)
	}
	key := policy.Keys[next.KeyID]
	key.Revoked = true
	policy.Keys[next.KeyID] = key
	if err := h.Check(next, policy); !errors.Is(err, release.ErrSignature) {
		t.Fatal("accepted history bypassed key revocation", err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Accept(context.Background(), next, policy); !errors.Is(err, ErrHistory) {
		t.Fatal("closed history allowed acceptance", err)
	}
}

func TestAcceptanceHistoryRecoversOnlyOneAuthenticatedUncheckpointedReservation(t *testing.T) {
	for _, pendingHead := range []bool{false, true} {
		t.Run(map[bool]string{false: "event-only", true: "event-and-next-checkpoint"}[pendingHead], func(t *testing.T) {
			options, signed, policy, _ := historyFixture(t)
			h, err := CreateHistory(options)
			if err != nil {
				t.Fatal(err)
			}
			event := historyEvent{Kind: "release-acceptance-v1", NodeID: options.NodeID, Number: 1, Release: signed}
			data, err := h.encode(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := exclusiveRoot(h.root, eventName(1), data); err != nil {
				t.Fatal(err)
			}
			if pendingHead {
				next, _ := h.encode(historyHead{Kind: h.head.Kind, NodeID: options.NodeID, Number: 1, Digest: eventDigest(data)})
				if err := exclusiveRoot(h.anchor, h.anchorName+".next", next); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.Check(signed, policy); !errors.Is(err, ErrHistory) {
				t.Fatal("unfinished commit reused old high water", err)
			}
			h.Close()
			h, err = OpenHistory(options)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			if h.head.Number != 1 || h.highest["proxy-agent"].Sequence != 1 {
				t.Fatal("reservation not recovered")
			}
			if err := h.Accept(context.Background(), signed, policy); err != nil {
				t.Fatal(err)
			}
			if h.head.Number != 1 {
				t.Fatal("recovery marked extra acceptance")
			}
		})
	}
}

func TestAcceptanceHistoryRejectsLostReplacedOrCorruptState(t *testing.T) {
	for _, fault := range []string{"missing-event", "missing-head", "old-head", "corrupt-event", "wrong-key", "wrong-node", "extra-file"} {
		t.Run(fault, func(t *testing.T) {
			options, signed, policy, private := historyFixture(t)
			h, err := CreateHistory(options)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Accept(context.Background(), signed, policy); err != nil {
				t.Fatal(err)
			}
			oldHead, err := os.ReadFile(options.Checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			m := signed.Manifest
			m.Sequence++
			next, _ := release.Sign(m, private)
			if err := h.Accept(context.Background(), next, policy); err != nil {
				t.Fatal(err)
			}
			h.Close()
			switch fault {
			case "missing-event":
				err = os.Remove(filepath.Join(options.Directory, eventName(2)))
			case "missing-head":
				err = os.Remove(options.Checkpoint)
			case "old-head":
				err = os.WriteFile(options.Checkpoint, oldHead, 0600)
			case "corrupt-event":
				err = os.WriteFile(filepath.Join(options.Directory, eventName(2)), []byte("corrupt"), 0600)
			case "wrong-key":
				options.Key = bytes.Repeat([]byte{0x43}, 32)
			case "wrong-node":
				options.NodeID = "different-node"
			case "extra-file":
				err = os.WriteFile(filepath.Join(options.Directory, "script.sh"), []byte("unexpected"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			opened, err := OpenHistory(options)
			// A one-event old head is exactly the authenticated interrupted
			// reservation case; it must restore the newer high water.
			if fault == "old-head" {
				if err != nil {
					t.Fatal(err)
				}
				defer opened.Close()
				if err := opened.Check(signed, policy); !errors.Is(err, ErrReplay) {
					t.Fatal("old checkpoint lowered high water", err)
				}
			} else if err == nil {
				opened.Close()
				t.Fatal("damaged history opened")
			}
			if opened, err := CreateHistory(options); err == nil {
				opened.Close()
				t.Fatal("existing history reinitialized")
			}
		})
	}
}

func TestAcceptanceFailureRetainsReservationAndDoesNotAcceptLowerRelease(t *testing.T) {
	options, signed, policy, private := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Accept(context.Background(), signed, policy); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(options.Checkpoint+".next", 0700); err != nil {
		t.Fatal(err)
	}
	m := signed.Manifest
	m.Sequence++
	next, _ := release.Sign(m, private)
	if err := h.Accept(context.Background(), next, policy); err == nil {
		t.Fatal("already damaged checkpoint accepted a new release")
	}
	if _, err := os.Stat(filepath.Join(options.Directory, eventName(2))); !os.IsNotExist(err) {
		t.Fatal("pre-existing checkpoint fault wrote an event", err)
	}
	// Reproduce a filesystem failure occurring after the durable event has
	// been written, at the same boundary exercised by Accept's commit path.
	event := historyEvent{Kind: "release-acceptance-v1", NodeID: options.NodeID, Number: 2, Previous: h.head.Digest, Release: next}
	data, err := h.encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := exclusiveRoot(h.root, eventName(2), data); err != nil {
		t.Fatal(err)
	}
	if err := h.writeHead(historyHead{Kind: h.head.Kind, NodeID: options.NodeID, Number: 2, Digest: eventDigest(data)}); err == nil {
		t.Fatal("actual checkpoint write fault succeeded")
	}
	if err := h.Check(signed, policy); !errors.Is(err, ErrHistory) {
		t.Fatal("failed checkpoint allowed older retry", err)
	}
	h.Close()
	if err := os.Remove(options.Checkpoint + ".next"); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Check(signed, policy); !errors.Is(err, ErrReplay) {
		t.Fatal("reserved sequence erased on recovery", err)
	}
}

func TestPendingCheckpointCannotHideMissingReservedEvent(t *testing.T) {
	options, signed, policy, private := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Accept(context.Background(), signed, policy); err != nil {
		t.Fatal(err)
	}
	m := signed.Manifest
	m.Sequence++
	next, _ := release.Sign(m, private)
	event := historyEvent{Kind: "release-acceptance-v1", NodeID: options.NodeID, Number: 2, Previous: h.head.Digest, Release: next}
	data, _ := h.encode(event)
	checkpoint, _ := h.encode(historyHead{Kind: h.head.Kind, NodeID: options.NodeID, Number: 2, Digest: eventDigest(data)})
	if err := exclusiveRoot(h.anchor, h.anchorName+".next", checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := h.Check(signed, policy); !errors.Is(err, ErrHistory) {
		t.Fatal("pending checkpoint ignored in live handle", err)
	}
	h.Close()
	opened, err := OpenHistory(options)
	if err == nil {
		opened.Close()
		t.Fatal("missing pending event downgraded history")
	}
}
