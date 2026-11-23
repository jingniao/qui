package statereset

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResetDeletionResumesOnlyBeforeRebuilt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filesystem sync semantics are Linux-only")
	}
	root := t.TempDir()
	data := filepath.Join(root, "data")
	journal := filepath.Join(root, "reset")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(journal, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &state{Phase: "sealed"}
	// Once deletion is durably entered, a partial deletion is safe to continue.
	if err := discard(journal, data, s); err != nil {
		t.Fatal(err)
	}
	if s.Phase != "deleted" {
		t.Fatalf("phase=%s", s.Phase)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("old data exists: %v", err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "new"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Phase = "rebuilt"
	if err := discard(journal, data, s); err == nil {
		t.Fatal("rebuilt state was eligible for deletion")
	}
	if _, err := os.Stat(filepath.Join(data, "new")); err != nil {
		t.Fatal(err)
	}
}

func TestResetDeletionRejectsAliasesAndOutsideRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filesystem sync semantics are Linux-only")
	}
	root := t.TempDir()
	data := filepath.Join(root, "data")
	journal := filepath.Join(data, "reset")
	if err := os.MkdirAll(journal, 0700); err != nil {
		t.Fatal(err)
	}
	if err := discard(journal, data, &state{Phase: "sealed"}); err == nil {
		t.Fatal("journal inside old directory accepted")
	}
}
