package reconcile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/pkg/contracts/desiredstate"
)

type refreshAdapter struct {
	fakeCoreAdapter
	fetcher            *rulesets.Fetcher
	directory, address string
}

func (a *refreshAdapter) PrepareRuleSets(ctx context.Context, _ desiredstate.State) (*rulesets.Update, error) {
	return a.fetcher.Ensure(ctx, a.directory, []rulesets.Need{{URL: a.address, SingBox: true, RefreshSeconds: 1}})
}
func (a *refreshAdapter) CleanupRuleSets(...desiredstate.State) {
	rulesets.Cleanup(a.directory, map[string]bool{rulesets.FileName(a.address, true): true})
}
func (a *refreshAdapter) Validate(context.Context, core.RenderedConfig) error {
	data, err := os.ReadFile(filepath.Join(rulesets.Dir(a.directory), rulesets.FileName(a.address, true)))
	if err != nil {
		return err
	}
	if string(data) != "SRS-good" {
		return errors.New("damaged native rule set")
	}
	return nil
}

func TestRuleSetRefreshRestoresAndRetriesValidationFailure(t *testing.T) {
	body, hits := "SRS-good", 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++; _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	a := &refreshAdapter{directory: t.TempDir(), address: server.URL + "/cn.srs", fetcher: &rulesets.Fetcher{Client: server.Client(), AllowHost: func(string) bool { return true }, AllowURL: func(string, bool) bool { return true }}}
	r, err := New(t.TempDir(), "node-test", a)
	if err != nil {
		t.Fatal(err)
	}
	s := state(freePort(t), 1)
	if result, err := r.Apply(context.Background(), s); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	file := filepath.Join(rulesets.Dir(a.directory), rulesets.FileName(a.address, true))
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	body = "SRS-damaged"
	s.Revision = 2
	if result, err := r.Apply(context.Background(), s); err == nil || result.Code != "validation_failed" {
		t.Fatal(result, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "SRS-good" {
		t.Fatalf("old rule set not restored: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(r.Directory, "revisions", "2", "result.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary native validation was cached: %v", err)
	}
	body = "SRS-good"
	if result, err := r.Apply(context.Background(), s); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if hits != 3 {
		t.Fatalf("retry did not download again: %d requests", hits)
	}
	if _, err := os.Stat(file + ".prev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup left after success: %v", err)
	}
}

func TestRuleSetRefreshRestoresEveryApplyFailure(t *testing.T) {
	for _, phase := range []string{"render", "switch", "health", "host-network", "storage"} {
		t.Run(phase, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("SRS-good")) }))
			defer server.Close()
			a := &refreshAdapter{directory: t.TempDir(), address: server.URL + "/cn.srs", fetcher: &rulesets.Fetcher{Client: server.Client(), AllowHost: func(string) bool { return true }, AllowURL: func(string, bool) bool { return true }}}
			r, err := New(t.TempDir(), "node-test", a)
			if err != nil {
				t.Fatal(err)
			}
			s := state(freePort(t), 1)
			if result, err := r.Apply(context.Background(), s); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			file := filepath.Join(rulesets.Dir(a.directory), rulesets.FileName(a.address, true))
			old := time.Now().Add(-time.Hour).Truncate(time.Second)
			if err := os.Chtimes(file, old, old); err != nil {
				t.Fatal(err)
			}
			switch phase {
			case "render":
				a.failRender = true
			case "switch":
				a.applyCodes = []string{"", "start_failed"}
			case "health":
				a.applyCodes = []string{"", "health_failed"}
			case "host-network":
				r.HostNetworkSync = func(context.Context, desiredstate.Network) error { return errors.New("helper failed") }
			case "storage":
				r.PrepareStorage = func(context.Context, desiredstate.State) error { return errors.New("storage failed") }
			}
			s.Revision = 2
			if result, err := r.Apply(context.Background(), s); err == nil || result.Success {
				t.Fatal("expected failure", result, err)
			}
			info, err := os.Stat(file)
			if err != nil || !info.ModTime().Equal(old) {
				t.Fatalf("previous file timestamp not restored: %v %v", info, err)
			}
			if _, err := os.Stat(file + ".prev"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("backup left after rollback", err)
			}
		})
	}
}

func TestRuleSetCommitCleanupFailureKeepsSuccessfulApply(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("SRS-good")) }))
	defer server.Close()
	a := &refreshAdapter{directory: t.TempDir(), address: server.URL + "/cn.srs", fetcher: &rulesets.Fetcher{Client: server.Client(), AllowHost: func(string) bool { return true }, AllowURL: func(string, bool) bool { return true }}}
	r, err := New(t.TempDir(), "node-test", a)
	if err != nil {
		t.Fatal(err)
	}
	r.RequireSuccessReceipts = true
	s := state(freePort(t), 1)
	for ; s.Revision <= 5; s.Revision++ {
		if result, err := r.Apply(context.Background(), s); err != nil || !result.Success {
			t.Fatal(result, err)
		}
	}
	file := filepath.Join(rulesets.Dir(a.directory), rulesets.FileName(a.address, true))
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(rulesets.Dir(a.directory), ".tmp-stale")
	if err := os.WriteFile(stale, []byte("unused"), 0600); err != nil {
		t.Fatal(err)
	}
	// A nonempty directory makes the real os.Remove fail on Windows and Unix,
	// without adding a production filesystem seam.
	r.HostNetworkSync = func(context.Context, desiredstate.Network) error {
		if err := os.Remove(file + ".prev"); err != nil {
			return err
		}
		if err := os.Mkdir(file+".prev", 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(file+".prev", "blocker"), []byte("busy"), 0600)
	}
	result, applyErr := r.Apply(context.Background(), s)
	var pathErr *os.PathError
	if !errors.As(applyErr, &pathErr) || pathErr.Path != file+".prev" {
		t.Fatalf("original cleanup error not exposed: %v", applyErr)
	}
	manifest, err := readCurrentManifest(r.Directory)
	if err != nil || manifest == nil || manifest.Revision != 6 || r.Current() != 6 {
		t.Fatalf("committed manifest: %+v %v", manifest, err)
	}
	receipt, err := r.readSuccessReceipt(6, manifest.Digest)
	if err != nil || receipt == nil || !receipt.Success {
		t.Fatalf("success receipt: %+v %v", receipt, err)
	}
	if !result.Success || result.Code != "success" || result.RolledBack {
		t.Errorf("committed apply reported as failure: %+v; cleanup error: %v", result, applyErr)
	}
	if a.applyCalls != 6 || a.applied[len(a.applied)-1].Revision != 6 {
		t.Fatal("new runtime was rolled back")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "SRS-good" {
		t.Fatalf("new rule set changed: %q %v", data, err)
	}
	if r.renderedDigest != digest(a.applied[len(a.applied)-1].Data) {
		t.Error("rendered digest was not advanced")
	}
	for _, path := range []string{stale, filepath.Join(r.Directory, "revisions", "1")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("subsequent cleanup skipped for %s: %v", path, err)
		}
	}
	replay, err := r.Apply(context.Background(), s)
	if err != nil || replay != *receipt {
		t.Fatalf("replay differs from success receipt: %+v %v", replay, err)
	}
	// Remove the injected obstruction; a later apply must clean the residue.
	if err := os.Remove(filepath.Join(file+".prev", "blocker")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file + ".prev"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file+".prev", []byte("SRS-old"), 0600); err != nil {
		t.Fatal(err)
	}
	r.HostNetworkSync = nil
	s.Revision++
	if result, err := r.Apply(context.Background(), s); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(file + ".prev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup residue not cleaned later: %v", err)
	}
}
