package lifecycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/platform/filelock"
)

func TestOwnershipBusyAndReleaseDoesNotRemovePermanentLocks(t *testing.T) {
	root := t.TempDir()
	first, err := Try(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := Try(filepath.Join(root, ".")); !errors.Is(err, filelock.ErrBusy) || second != nil {
		t.Fatal("second Agent accepted same data", err)
	}
	identityLock := filepath.Join(root, "identity", lockName)
	if lock, err := filelock.Try(identityLock); !errors.Is(err, filelock.ErrBusy) || lock != nil {
		t.Fatal("Agent identity was not locked", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, lockName), identityLock} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("permanent lock removed", err)
		}
	}
	second, err := Try(root)
	if err != nil {
		t.Fatal("replacement could not acquire released state", err)
	}
	second.Close()
}

func TestOwnershipPartialFailureReleasesRootAndRejectsHardlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	identityLock, err := filelock.Try(filepath.Join(root, "identity", lockName))
	if err != nil {
		t.Fatal(err)
	}
	defer identityLock.Close()
	if owner, err := Try(root); owner != nil || !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("shared identity was accepted", err)
	}
	rootLock, err := filelock.Try(filepath.Join(root, lockName))
	if err != nil {
		t.Fatal("failed acquisition leaked root ownership", err)
	}
	rootLock.Close()
	identityLock.Close()
	alias := filepath.Join(root, "external-alias")
	if err := os.Link(filepath.Join(root, lockName), alias); err != nil {
		t.Fatal(err)
	}
	if owner, err := Try(root); owner != nil || err == nil {
		t.Fatal("hardlinked lifecycle marker was accepted")
	}
}

func TestOwnershipDirectoryAliasUsesSameLock(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	owner, err := Try(data)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(data, alias); err != nil {
		t.Skip("directory symlinks unavailable", err)
	}
	if other, err := Try(alias); other != nil || !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("directory alias bypassed ownership", err)
	}
}

func TestOwnershipProcessDeathAllowsReplacement(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnershipChild$")
	command.Env = append(os.Environ(), "PROXY_AGENT_OWNERSHIP_TEST="+root)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	reader := bufio.NewScanner(stdout)
	if !reader.Scan() || reader.Text() != "owned" {
		command.Wait()
		t.Fatal("child did not own data", reader.Text(), reader.Err())
	}
	if other, err := Try(root); other != nil || !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("live child ownership bypassed", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("child did not terminate abnormally")
	}
	replacement, err := Try(root)
	if err != nil {
		t.Fatal("dead Agent permanently blocked replacement", err)
	}
	replacement.Close()
}

func TestOwnershipChild(t *testing.T) {
	root := os.Getenv("PROXY_AGENT_OWNERSHIP_TEST")
	if root == "" {
		t.Skip("child process only")
	}
	owner, err := Try(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	fmt.Println("owned")
	var data [1]byte
	os.Stdin.Read(data[:])
}
