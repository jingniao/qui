package agenthost

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"proxycontrol/internal/agent/lifecycle"
)

func initializeConfig(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	config := filepath.Join(root, "agent.yaml")
	data := filepath.Join(root, "data")
	contents := "development: true\ndata_directory: " + data + "\ncore_path: xray\n"
	if err := os.WriteFile(config, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return config, data
}

func TestInitializeManagedDirectoryPreservesState(t *testing.T) {
	config, data := initializeConfig(t)
	marker := filepath.Join(data, "identity", markerName)
	if err := os.MkdirAll(filepath.Dir(marker), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("managed")
	if err := os.WriteFile(marker, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(context.Background(), config, "1.5.0"); err == nil {
		t.Fatal("managed directory was accepted")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != string(original) {
		t.Fatalf("managed state changed: %q %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(data, pendingPath)); !os.IsNotExist(err) {
		t.Fatalf("pending file left behind: %v", err)
	}
}

func TestInitializeBusyDirectoryPreservesContents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file locking prevents opening the second supervisor lock in-process")
	}
	config, data := initializeConfig(t)
	owner, err := lifecycle.TrySupervisor(data)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	// Snapshot after acquiring the lock: the lock itself creates its files.
	before, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := Initialize(context.Background(), config, "1.5.0"); err == nil {
		t.Fatal("busy directory was accepted")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(data)
	if err != nil || len(after) != len(before) {
		t.Fatalf("busy directory changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(data, pendingPath)); !os.IsNotExist(err) {
		t.Fatalf("pending file left behind: %v", err)
	}
}

func TestInitializeUnenrolledDirectoryLeavesNoPending(t *testing.T) {
	config, data := initializeConfig(t)
	if err := Initialize(context.Background(), config, "1.5.0"); err == nil {
		t.Fatal("unenrolled directory was accepted")
	}
	if _, err := os.Lstat(filepath.Join(data, pendingPath)); !os.IsNotExist(err) {
		t.Fatalf("pending file left behind: %v", err)
	}
}
