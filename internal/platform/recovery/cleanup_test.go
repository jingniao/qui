package recovery

import (
	"context"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"testing"
)

func TestEvidenceAlreadyPublishedCanFinalizeWithoutLinkedCandidate(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	manifest := candidate.Manifest
	item := manifest.Files[len(manifest.Files)-1].File
	directory := filepath.Join(filepath.Dir(candidate.Directory), "restore-evidence")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, manifest.EvidenceID+".pcb")
	if err := copyNamed(ctx, filepath.Join(candidate.Directory, filepath.FromSlash(item.Name)), target, item); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCandidate(ctx, candidate, f.prepared); err != nil {
		t.Fatal("independent publication invalidated candidate", err)
	}
	result, err := FinalizeCandidate(ctx, candidate, f.prepared)
	if err != nil || result != target {
		t.Fatal("published evidence cannot be resumed", result, err)
	}
	if _, err := os.Stat(candidate.Directory); !os.IsNotExist(err) {
		t.Fatal("candidate plaintext remained", err)
	}
	if _, err := archive.Inspect(ctx, t.TempDir(), target, recoveryPassword); err != nil {
		t.Fatal("published evidence lost authentication", err)
	}
}
