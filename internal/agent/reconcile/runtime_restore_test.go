package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A retained revision the installed core cannot render (an operator switched
// sing-box to Xray under a Hysteria2 revision) is a runtime failure: the state
// itself is sound, so the Agent may stay up with the core stopped.
func TestRetainedRevisionTheCoreCannotRenderIsARuntimeFailure(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state(1080, 1)); err != nil || !result.Success {
		t.Fatalf("first apply: %+v %v", result, err)
	}
	adapter.failRenderRevision = 1
	restarted, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	err = restarted.Restore(ctx)
	if !errors.Is(err, ErrRuntimeRestore) {
		t.Fatalf("runtime restore failure not marked: %v", err)
	}
	if !strings.Contains(err.Error(), "render failed") {
		t.Fatalf("inner render error lost: %v", err)
	}
}

// Integrity failures must stay indistinguishable from the fatal path: a tampered
// retained revision is not something a new revision can repair.
func TestTamperedRetainedRevisionIsNotARuntimeFailure(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state(1080, 1)); err != nil || !result.Success {
		t.Fatalf("first apply: %+v %v", result, err)
	}
	path := filepath.Join(directory, "revisions", "1", "desired.json.enc")
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if err := os.WriteFile(path, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	err = restarted.Restore(ctx)
	if err == nil || errors.Is(err, ErrRuntimeRestore) {
		t.Fatalf("tampered revision reported as a runtime failure: %v", err)
	}
}

// A failed runtime restore must not block the next revision the panel sends,
// which is how the operator recovers a node after changing its core.
func TestNewRevisionAppliesAfterAFailedRuntimeRestore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state(1080, 1)); err != nil || !result.Success {
		t.Fatalf("first apply: %+v %v", result, err)
	}
	adapter.failRenderRevision = 1
	restarted, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(ctx); !errors.Is(err, ErrRuntimeRestore) {
		t.Fatalf("restore: %v", err)
	}
	result, err := restarted.Apply(ctx, state(1081, 2))
	if err != nil || !result.Success {
		t.Fatalf("new revision after a failed restore: %+v %v", result, err)
	}
	if restarted.Current() != 2 {
		t.Fatalf("new revision not committed: %d", restarted.Current())
	}
}

// Re-applying the retained revision while the installed core cannot render it is
// reported without touching disk: the revision directory is the only surviving
// copy, and a cached failure receipt there would make it unrestorable.
func TestRetainedRevisionIsNotDestroyedByARenderFailure(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(1080, 1)
	if result, err := manager.Apply(ctx, configuration); err != nil || !result.Success {
		t.Fatalf("first apply: %+v %v", result, err)
	}
	before := revisionFiles(t, directory)
	adapter.failRenderRevision = 1
	adapter.failHealth = true
	result, err := manager.Apply(ctx, configuration)
	if err == nil || result.Code != "render_failed" {
		t.Fatalf("render failure not reported: %+v %v", result, err)
	}
	after := revisionFiles(t, directory)
	if len(before) != len(after) {
		t.Fatalf("retained revision changed: %v then %v", keys(before), keys(after))
	}
	for name, data := range before {
		if after[name] != data {
			t.Fatalf("retained revision file %s changed", name)
		}
	}
	if _, ok := after["revisions/1/result.json"]; ok {
		t.Fatal("failure receipt written into the retained revision")
	}
}

// Re-applying the retained revision while its core cannot start it is the other
// way to destroy the only copy a later startup can restore: a removed directory,
// or one holding only a failure receipt, turns the next Restore into a read error,
// which is fatal. Every failure of that apply must leave the files in place.
func TestRetainedRevisionIsNotDestroyedByAFailedApply(t *testing.T) {
	cases := []struct{ name, code string }{
		{"temporary start failure", "start_failed"},
		{"permanent failure code", "render_failed"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			adapter := &fakeCoreAdapter{applyCodes: []string{"", test.code}}
			manager, err := New(directory, "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			configuration := state(1080, 1)
			if result, err := manager.Apply(ctx, configuration); err != nil || !result.Success {
				t.Fatalf("first apply: %+v %v", result, err)
			}
			revisionDir := filepath.Join(directory, "revisions", "1")
			currentBefore, err := os.ReadFile(filepath.Join(directory, "current.json"))
			if err != nil {
				t.Fatal(err)
			}
			// Keep Health unhealthy, otherwise the re-apply answers "already
			// applied" from the running core and never reaches the adapter.
			adapter.failHealth = true
			result, err := manager.Apply(ctx, configuration)
			if err == nil || result.Code != test.code {
				t.Fatalf("retained re-apply failure not reported: %+v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(revisionDir, "desired.json.enc")); err != nil {
				t.Fatalf("retained revision removed: %v", err)
			}
			currentAfter, err := os.ReadFile(filepath.Join(directory, "current.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(currentAfter) != string(currentBefore) {
				t.Fatal("current manifest changed by a failed retained re-apply")
			}
			if _, err := os.Lstat(filepath.Join(revisionDir, "result.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failure receipt written into the retained revision: %v", err)
			}
			// The next startup with a core that can run the revision must work.
			restarted, err := New(directory, "node-test", &fakeCoreAdapter{})
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.Restore(ctx); err != nil {
				t.Fatalf("restore after a failed retained re-apply: %v", err)
			}
		})
	}
}

// revisionFiles snapshots every retained file of the node directory, keyed by
// slash-separated relative path.
func revisionFiles(t *testing.T, directory string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func keys(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}
