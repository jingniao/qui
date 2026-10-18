package filelock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLockBusyAndReleasedAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.lock")
	first, err := Try(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := Try(path); !errors.Is(err, ErrBusy) || second != nil {
		t.Fatal("same-process second handle acquired owned lock", err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestFileLockChild$")
	command.Env = append(os.Environ(), "PROXY_LOCK_TEST_PATH="+path, "PROXY_LOCK_EXPECT_BUSY=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal("another process acquired live store lock", err, string(output))
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	command = exec.Command(os.Args[0], "-test.run=^TestFileLockChild$")
	command.Env = append(os.Environ(), "PROXY_LOCK_TEST_PATH="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal("closed lock did not become available", err, string(output))
	}
}
func TestFileLockChild(t *testing.T) {
	path := os.Getenv("PROXY_LOCK_TEST_PATH")
	if path == "" {
		t.Skip("only executed by process-lock parent")
	}
	lock, err := Try(path)
	if os.Getenv("PROXY_LOCK_EXPECT_BUSY") == "1" {
		if !errors.Is(err, ErrBusy) || lock != nil {
			t.Fatal("live lock was not exclusive", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
}

func TestLinkedLockFileCannotAliasAnotherStore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "store.lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "other.lock")
	if err := os.Link(path, alias); err != nil {
		t.Skip("hardlinks unavailable", err)
	}
	if lock, err := Try(path); lock != nil || err == nil {
		t.Fatal("hardlinked permanent lock accepted", err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	lock, err := Try(path)
	if err != nil {
		t.Fatal("failed link check leaked open lock", err)
	}
	lock.Close()
}
