package recovery

import (
	"context"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
)

// FinalizeCandidate retains the authenticated encrypted current-state evidence
// outside the plaintext candidate, then removes the verified candidate material.
// It is called only after file commitment and before accepting service writes.
func FinalizeCandidate(ctx context.Context, candidate DatabaseCandidate, prepared archive.PreparedRestore) (string, error) {
	manifest, err := VerifyCandidate(ctx, candidate, prepared)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(filepath.Dir(candidate.Directory), "restore-evidence")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrEvidence
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	source := filepath.Join(candidate.Directory, "evidence", manifest.EvidenceID+".pcb")
	target := filepath.Join(directory, manifest.EvidenceID+".pcb")
	file := manifest.Files[len(manifest.Files)-1].File
	if _, err := os.Lstat(target); err == nil {
		existing, err := digest(ctx, target, file.Bytes)
		if err != nil || existing.Bytes != file.Bytes || existing.SHA256 != file.SHA256 {
			return "", ErrEvidence
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else {
		temporary, err := os.MkdirTemp(directory, ".publish-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(temporary)
		partial := filepath.Join(temporary, "evidence.pcb")
		if err := copyNamed(ctx, source, partial, file); err != nil {
			return "", err
		}
		// Publication is serialized by panel lifetime ownership. The rename
		// leaves an independent file even if the process exits immediately.
		if err := os.Rename(partial, target); err != nil {
			return "", err
		}
	}
	if err := removeCandidate(candidate.Directory); err != nil {
		return target, err
	}
	return target, nil
}

func DiscardCandidate(ctx context.Context, candidate DatabaseCandidate, prepared archive.PreparedRestore) error {
	if _, err := VerifyCandidate(ctx, candidate, prepared); err != nil {
		return err
	}
	return removeCandidate(candidate.Directory)
}
