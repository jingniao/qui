package hostupdatestate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscardSnapshotPhases(t *testing.T) {
	t.Run("final directory sync", func(t *testing.T) {
		d := privateDir(t)
		src := filepath.Join(d, "rollback.final-sync")
		os.Mkdir(src, 0700)
		boom := errors.New("final sync")
		calls := 0
		err := discardSnapshot(d, src, fileOperations{sync: func(f *os.File) error {
			calls++
			if calls == 2 {
				return boom
			}
			return f.Sync()
		}})
		if !errors.Is(err, boom) {
			t.Fatal(err)
		}
		if _, err = os.Stat(filepath.Join(d, "discard.final-sync")); !os.IsNotExist(err) {
			t.Fatal(err)
		}
	})
	t.Run("replacement race", func(t *testing.T) {
		d := privateDir(t)
		src := filepath.Join(d, "rollback.race")
		os.Mkdir(src, 0700)
		err := discardSnapshot(d, src, fileOperations{pinned: func() error {
			if err := os.Rename(src, src+".saved"); err != nil {
				return err
			}
			return os.Mkdir(src, 0700)
		}})
		if err == nil {
			t.Fatal("accepted replaced snapshot")
		}
		if _, err = os.Stat(src); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(src + ".saved"); err != nil {
			t.Fatal(err)
		}
	})
	for _, stage := range []string{"success", "rename", "sync", "remove", "conflict"} {
		t.Run(stage, func(t *testing.T) {
			d := privateDir(t)
			src := filepath.Join(d, "rollback.historical-suffix")
			dst := filepath.Join(d, "discard.historical-suffix")
			os.Mkdir(src, 0700)
			os.WriteFile(filepath.Join(src, "journal"), []byte("material"), 0600)
			boom := errors.New(stage)
			ops := fileOperations{}
			switch stage {
			case "rename":
				ops.rename = func(int, string, int, string) error { return boom }
			case "sync":
				ops.sync = func(*os.File) error { return boom }
			case "remove":
				ops.remove = func(*os.Root, string) error { return boom }
			case "conflict":
				os.Mkdir(dst, 0700)
				os.WriteFile(filepath.Join(dst, "other"), []byte("other"), 0600)
			}
			err := discardSnapshot(d, src, ops)
			if stage == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if err = DiscardSnapshot(d, src); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected failure")
			}
			preserved := src
			if stage == "sync" || stage == "remove" {
				preserved = dst
			}
			if _, err = os.Stat(filepath.Join(preserved, "journal")); err != nil {
				t.Fatal(err)
			}
			if stage == "conflict" {
				if _, err = os.Stat(filepath.Join(dst, "other")); err != nil {
					t.Fatal(err)
				}
			}
			if preserved == dst {
				if err = RemoveDiscarded(d, dst); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRemoveDiscarded(t *testing.T) {
	d := privateDir(t)
	outside := privateDir(t)
	os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0600)
	child := filepath.Join(d, "discard.nested-symlink")
	os.Mkdir(child, 0700)
	os.Symlink(outside, filepath.Join(child, "link"))
	if err := RemoveDiscarded(d, child); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, filepath.Join(d, "discard."), filepath.Join(d, "nested", "discard.x"), filepath.Join(d, "rollback.x")} {
		if err := RemoveDiscarded(d, path); err == nil {
			t.Fatal("accepted", path)
		}
	}
	sym := filepath.Join(d, "discard.link")
	os.Symlink(outside, sym)
	if err := RemoveDiscarded(d, sym); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal(err)
	}
}
