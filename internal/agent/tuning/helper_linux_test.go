//go:build linux

package tuning

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestPrivilegedRequestDirectoryCannotEscape(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "requests")
	moved := filepath.Join(parent, "moved")
	outside := t.TempDir()
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := requestRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if err := writeStatusRoot(root, &pb.TuningResult{RequestId: "pinned", Status: "applied"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, "status.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "status.json")); !os.IsNotExist(err) {
		t.Fatal("status escaped pinned directory", err)
	}
	if _, err := requestRoot(dir); err == nil {
		t.Fatal("accepted directory symlink")
	}
}

func TestDesiredCannotBeSymlinkOrBlockingFIFO(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	put(t, outside, `{"request_id":"outside"}`)
	desired := filepath.Join(dir, "desired.json")
	if err := os.Symlink(outside, desired); err != nil {
		t.Fatal(err)
	}
	if _, err := readDesired(dir); err == nil {
		t.Fatal("read outside request directory")
	}
	if err := os.Remove(desired); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(desired, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDesired(dir); err == nil {
		t.Fatal("accepted FIFO")
	}
}
