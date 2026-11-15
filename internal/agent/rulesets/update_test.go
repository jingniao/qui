package rulesets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRuleSetUpdateStagesRestoresAndCommits(t *testing.T) {
	for _, success := range []bool{false, true} {
		f, runtime := newFixture(t), t.TempDir()
		if err := os.MkdirAll(Dir(runtime), 0700); err != nil {
			t.Fatal(err)
		}
		file := f.file(runtime)
		if err := os.WriteFile(file, []byte("SRS-old"), 0600); err != nil {
			t.Fatal(err)
		}
		need := f.need()
		need.RefreshSeconds = 0
		update, err := f.fetcher.Ensure(context.Background(), runtime, []Need{need})
		if err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(file); string(data) != "SRS-old" {
			t.Fatal("preparation replaced live content")
		}
		if err := update.Activate(); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(file + ".prev"); string(data) != "SRS-old" {
			t.Fatal("previous content not retained")
		}
		if success {
			if err := update.Commit(); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(file); string(data) != string(srsBody(10)) {
				t.Fatal("commit lost new content")
			}
		} else {
			if err := update.Rollback(); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(file); string(data) != "SRS-old" {
				t.Fatal("rollback lost previous content")
			}
			if err := update.Rollback(); err != nil {
				t.Fatal("repeated rollback", err)
			}
		}
		if _, err := os.Stat(file + ".prev"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("previous file left", err)
		}
	}
}

func TestRuleSetPartialActivationAndFirstDownloadRollback(t *testing.T) {
	dir := t.TempDir()
	old, first, second := filepath.Join(dir, "old"), filepath.Join(dir, "new"), filepath.Join(dir, "second")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	update := &Update{files: []replacement{{target: old, temporary: first}, {target: filepath.Join(dir, "missing", "second"), temporary: second}}}
	if err := update.Activate(); err == nil {
		t.Fatal("expected missing parent error")
	}
	if err := update.Rollback(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(old); string(data) != "old" {
		t.Fatal("partially replaced file not restored")
	}
	f, runtime := newFixture(t), t.TempDir()
	update, err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()})
	if err != nil {
		t.Fatal(err)
	}
	if err := update.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := update.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.file(runtime)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed first download remained cached", err)
	}
}

func TestRuleSetCleanupRemovesPreviousFiles(t *testing.T) {
	runtime := t.TempDir()
	if err := os.MkdirAll(Dir(runtime), 0700); err != nil {
		t.Fatal(err)
	}
	name := FileName("https://example.com/cn.srs", true)
	for _, suffix := range []string{"", ".prev"} {
		if err := os.WriteFile(filepath.Join(Dir(runtime), name+suffix), []byte("SRS"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	Cleanup(runtime, map[string]bool{name: true})
	if _, err := os.Stat(filepath.Join(Dir(runtime), name)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir(runtime), name+".prev")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale previous file retained", err)
	}
}
