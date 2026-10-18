package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/filelock"
	"reflect"
	"strings"
	"time"
)

// VerifyCandidate authenticates the material list against independent handles
// retained by the coordinator. It does not declare the live evidence current
// or permit a file replacement after writers have resumed.
func VerifyCandidate(ctx context.Context, candidate DatabaseCandidate, prepared archive.PreparedRestore) (CandidateManifest, error) {
	var manifest CandidateManifest
	plan, err := archive.VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256)
	if err != nil {
		return manifest, err
	}
	info, err := os.Lstat(candidate.Directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !strings.HasPrefix(filepath.Base(candidate.Directory), ".recovery-") {
		return manifest, ErrEvidence
	}
	if !validHash(candidate.ManifestSHA256) {
		return manifest, ErrEvidence
	}
	marker, err := readLimited(candidate.Directory, ".panel-recovery-candidate", 128)
	if err != nil || string(marker) != "proxy-control-recovery-candidate-v1\n" {
		return manifest, ErrEvidence
	}
	data, err := readLimited(candidate.Directory, "candidate.json", 1<<20)
	if err != nil {
		return manifest, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != candidate.ManifestSHA256 {
		return manifest, ErrEvidence
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) != io.EOF || manifest.Version != 1 {
		return CandidateManifest{}, ErrEvidence
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.PreparedAt); err != nil {
		return CandidateManifest{}, ErrEvidence
	}
	if manifest.SourcePlanSHA256 != prepared.ManifestSHA256 || manifest.SnapshotID != plan.Source.ID || manifest.ArchiveSHA256 != plan.ArchiveSHA256 || !reflect.DeepEqual(manifest.Target, plan.Target) || !validHash(manifest.EvidenceSHA256) || !validRecoveryID(manifest.EvidenceID) || len(manifest.Files) != len(plan.Files)+1 {
		return CandidateManifest{}, ErrEvidence
	}
	allowed := map[string]bool{".panel-recovery-candidate": true, "candidate.json": true}
	directories := map[string]bool{".": true}
	for index, item := range manifest.Files {
		if !validHash(item.SHA256) || item.Bytes < 0 || item.Bytes > archive.MaxArchiveBytes {
			return CandidateManifest{}, ErrEvidence
		}
		if index < len(plan.Files) {
			original := plan.Files[index]
			if item.Name != original.Name || item.Destination != original.Destination {
				return CandidateManifest{}, ErrEvidence
			}
			if item.Name != "database/panel.db" && item.File != original.File {
				return CandidateManifest{}, ErrEvidence
			}
		} else if item.Name != "evidence/"+manifest.EvidenceID+".pcb" || item.Destination != "" || item.SHA256 != manifest.EvidenceSHA256 {
			return CandidateManifest{}, ErrEvidence
		}
		allowed[item.Name] = true
		for parent := filepath.Dir(filepath.FromSlash(item.Name)); parent != "."; parent = filepath.Dir(parent) {
			directories[filepath.ToSlash(parent)] = true
		}
		file, err := digest(ctx, filepath.Join(candidate.Directory, filepath.FromSlash(item.Name)), item.Bytes)
		if err != nil || file.Bytes != item.Bytes || file.SHA256 != item.SHA256 {
			return CandidateManifest{}, ErrEvidence
		}
	}
	err = filepath.WalkDir(candidate.Directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(candidate.Directory, path)
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			return ErrEvidence
		}
		name := filepath.ToSlash(relative)
		if entry.IsDir() {
			if !directories[name] {
				return ErrEvidence
			}
			return nil
		}
		if !entry.Type().IsRegular() || !allowed[name] {
			return ErrEvidence
		}
		return nil
	})
	if err != nil {
		return CandidateManifest{}, err
	}
	if err := archive.ValidateMaterialSet(ctx, candidate.Directory); err != nil {
		return CandidateManifest{}, err
	}
	inventory, err := archive.InventoryFromVerifiedDirectory(ctx, candidate.Directory)
	if err != nil || inventory.Totals != manifest.Candidate {
		return CandidateManifest{}, ErrEvidence
	}
	if inventory.Totals.ActiveOwners != 1 || inventory.Totals.Sessions != 0 || inventory.Totals.PendingOperations != 0 || inventory.Totals.BackupFileOperations != 0 || inventory.Totals.RunningBackupJobs != 0 {
		return CandidateManifest{}, ErrEvidence
	}
	return manifest, nil
}

func readLimited(directory, name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrEvidence
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := filelock.CheckSingleLink(file); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrEvidence
	}
	return data, nil
}
func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
func validRecoveryID(value string) bool {
	if len(value) < 16 || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
