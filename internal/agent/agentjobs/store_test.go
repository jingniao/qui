package agentjobs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/release"
)

func fixture(t *testing.T) (Options, release.AgentUpdateRequest) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	o := Options{Directory: filepath.Join(root, "jobs"), Checkpoint: filepath.Join(root, "identity", "jobs.enc"), NodeID: "node", Key: bytes.Repeat([]byte{37}, 32)}
	if err := Initialize(o); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "1.0", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 1, Bytes: 128, SHA256: strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	return o, release.AgentUpdateRequest{SchemaVersion: 1, ID: "install-one", NodeID: "node", Revision: 1, Operation: "install", Release: signed}
}
func openTest(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func success(r release.AgentUpdateRequest) release.UpdateResult {
	return release.UpdateResult{ID: r.ID, NodeID: r.NodeID, Revision: r.Revision, TargetDigest: r.Release.Manifest.SHA256, Status: "success", Code: "success", InstalledDigest: r.Release.Manifest.SHA256, InstalledVersion: r.Release.Manifest.Version, InstalledSequence: r.Release.Manifest.Sequence}
}

func TestDurableAgentJobsBindFullRequestAndImmutableResult(t *testing.T) {
	o, r := fixture(t)
	s := openTest(t, o)
	if err := s.Queue(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Queue(r); err != nil {
		t.Fatal("identical request rejected", err)
	}
	changed := r
	changed.Release.Signature = "changed"
	if err := s.Queue(changed); err == nil {
		t.Fatal("same ID with changed command accepted")
	}
	second := r
	second.ID = "install-two"
	if err := s.Queue(second); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent pending request accepted", err)
	}
	s.Close()
	s = openTest(t, o)
	if pending := s.Pending(); pending == nil || pending.Request != r {
		t.Fatal("pending request lost")
	}
	if err := s.MarkStarted(r); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, o)
	if pending := s.Pending(); pending == nil || !pending.Started || pending.Result != nil {
		t.Fatal("execution binding did not survive")
	}
	result := success(r)
	if err := s.Finish(r, result); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(r, result); err != nil {
		t.Fatal("idempotent result rejected", err)
	}
	changedResult := result
	changedResult.Status = "rejected"
	changedResult.Code = "trust_failed"
	if err := s.Finish(r, changedResult); err == nil {
		t.Fatal("historical result changed")
	}
	s.Close()
	s = openTest(t, o)
	defer s.Close()
	job, ok := s.Get(r.ID)
	if !ok || job.Result == nil || *job.Result != result || s.Pending() != nil {
		t.Fatal("result did not survive restart")
	}
}

func TestCompleteInterruptedTailIsRecoveredWithoutInventingOutcome(t *testing.T) {
	o, r := fixture(t)
	before, err := Fingerprint(o)
	if err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile(o.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	s := openTest(t, o)
	if err := s.Queue(r); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.WriteFile(o.Checkpoint, head, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := Fingerprint(o)
	if err != nil || before == after {
		t.Fatal("interrupted tail invisible to polling", err)
	}
	s = openTest(t, o)
	defer s.Close()
	if pending := s.Pending(); pending == nil || pending.Request != r || pending.Result != nil {
		t.Fatal("request tail became success")
	}
}

func TestMissingOrDamagedHistoryDoesNotResetAndCheckpointChangesRefused(t *testing.T) {
	o, r := fixture(t)
	s := openTest(t, o)
	if err := s.Queue(r); err != nil {
		t.Fatal(err)
	}
	s.Close()
	eventPath := filepath.Join(o.Directory, eventName(1))
	sealed, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(eventPath); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(o); err == nil {
		bad.Close()
		t.Fatal("missing history reset")
	}
	if err := Initialize(o); err == nil {
		t.Fatal("existing checkpoint reinitialized")
	}
	if err := os.WriteFile(eventPath, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, o)
	if err := os.WriteFile(o.Checkpoint, []byte("damaged checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(r, success(r)); err == nil {
		t.Fatal("open store overwrote changed checkpoint")
	}
	s.Close()
	retained, _ := os.ReadFile(o.Checkpoint)
	if string(retained) != "damaged checkpoint" {
		t.Fatal("damaged evidence overwritten")
	}
}

func TestAgentJobsRejectForeignIdentityKeyAndResultScope(t *testing.T) {
	o, r := fixture(t)
	foreign := o
	foreign.NodeID = "other"
	if s, err := Open(foreign); err == nil {
		s.Close()
		t.Fatal("foreign node opened journal")
	}
	foreign = o
	foreign.Key = bytes.Repeat([]byte{38}, 32)
	if s, err := Open(foreign); err == nil {
		s.Close()
		t.Fatal("foreign key opened journal")
	}
	s := openTest(t, o)
	defer s.Close()
	if err := s.Queue(r); err != nil {
		t.Fatal(err)
	}
	result := success(r)
	if err := s.MarkStarted(r); err != nil {
		t.Fatal(err)
	}
	result.Revision++
	if err := s.Finish(r, result); err == nil {
		t.Fatal("wrong revision result recorded")
	}
}

func TestOversizedCommandDoesNotConsumeJournalCapacity(t *testing.T) {
	o, r := fixture(t)
	s := openTest(t, o)
	defer s.Close()
	r.Release.Signature = strings.Repeat("x", release.MaxUpdateRequestBytes)
	if err := s.Queue(r); err == nil {
		t.Fatal("oversized command accepted")
	}
	if s.head.Number != 0 || s.Pending() != nil {
		t.Fatal("invalid request mutated journal")
	}
}
