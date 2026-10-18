package agenthost

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/agent/updater"
)

func TestPartialSupervisionNeverLooksLikeStandalone(t *testing.T) {
	for _, artifact := range []string{"identity/" + markerName, "identity/agent-release-checkpoint.enc", "agent-release-history", "releases/agent-installed"} {
		t.Run(artifact, func(t *testing.T) {
			root := t.TempDir()
			if active, err := Active(root); err != nil || active {
				t.Fatal(active, err)
			}
			path := filepath.Join(root, filepath.FromSlash(artifact))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("partial evidence"), 0600); err != nil {
				t.Fatal(err)
			}
			if active, err := Active(root); err != nil || !active {
				t.Fatal("partial state became standalone", active, err)
			}
		})
	}
}

func TestSupervisorRecordOriginalKeyNodeAndImmutableEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), markerName)
	key := bytes.Repeat([]byte{17}, 32)
	r := record{Kind: "agent-supervision-v1", NodeID: "node", Baseline: updater.Image{SHA256: strings.Repeat("a", 64), Version: "1.0", Bytes: 100}}
	if err := createRecord(path, key, r); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readRecord(path, key, "node"); err != nil || got != r {
		t.Fatal(got, err)
	}
	if err := createRecord(path, key, r); err == nil {
		t.Fatal("original activation overwritten")
	}
	if _, err := readRecord(path, key, "other"); err == nil {
		t.Fatal("foreign node accepted")
	}
	if _, err := readRecord(path, bytes.Repeat([]byte{18}, 32), "node"); err == nil {
		t.Fatal("foreign key accepted")
	}
	if retained, err := os.ReadFile(path); err != nil || !bytes.Equal(original, retained) {
		t.Fatal("failed read changed evidence", err)
	}
	if err := os.Link(path, path+".alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecord(path, key, "node"); err == nil {
		t.Fatal("aliased marker accepted")
	}
	if err := os.Remove(path + ".alias"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("damaged-record"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecord(path, key, "node"); err == nil {
		t.Fatal("damaged marker accepted")
	}
	if retained, err := os.ReadFile(path); err != nil || string(retained) != "damaged-record" {
		t.Fatal("damaged evidence replaced", err)
	}
}
