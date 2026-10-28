// Package recovery orchestrates offline recovery through table-owning modules.
package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/panelstore"
	"time"
)

var ErrEvidence = errors.New("restore requires a matching, quiescent current database and identity")

type CandidateManifest struct {
	Version          int                   `json:"version"`
	PreparedAt       string                `json:"preparedAt"`
	SourcePlanSHA256 string                `json:"sourcePlanSha256"`
	SnapshotID       string                `json:"snapshotId"`
	ArchiveSHA256    string                `json:"archiveSha256"`
	EvidenceID       string                `json:"evidenceId"`
	EvidenceSHA256   string                `json:"evidenceSha256"`
	Target           archive.RestoreTarget `json:"target"`
	Files            []archive.RestoreFile `json:"files"`
	Snapshot         archive.Totals        `json:"snapshot"`
	Current          archive.Totals        `json:"current"`
	Candidate        archive.Totals        `json:"candidate"`
}

type DatabaseCandidate struct {
	Directory      string
	ManifestSHA256 string
	Manifest       CandidateManifest
}

type candidateEvidence struct {
	Directory string
	Result    archive.Result
}

// Settled preparation pins its original source before candidate mutation and
// can rebuild a retained failed attempt from that same authenticated evidence.
type candidatePreparation struct {
	Original   *candidateEvidence
	OnEvidence func(candidateEvidence) error
}

// PrepareDatabase requires the caller to have stopped its own HTTP/gRPC and
// all writers while holding lifetime ownership. Only the candidate is edited.
// Missing/foreign current evidence and unfinished deployment execution remain
// explicit reconciliation conditions, not permission to reset counters to zero.
func PrepareDatabase(ctx context.Context, owner *panelstore.Ownership, current archive.Source, prepared archive.PreparedRestore, password string) (candidate DatabaseCandidate, resultErr error) {
	return prepareDatabase(ctx, owner, current, prepared, password, "", false)
}

// A settled-candidate request owns a fixed directory and retains failed
// material. The ordinary local restore path keeps its existing lifecycle.
func prepareDatabase(ctx context.Context, owner *panelstore.Ownership, current archive.Source, prepared archive.PreparedRestore, password, work string, retainFailure bool, options ...candidatePreparation) (candidate DatabaseCandidate, resultErr error) {
	var preparation candidatePreparation
	if len(options) > 1 {
		return candidate, ErrEvidence
	}
	if len(options) == 1 {
		preparation = options[0]
	}
	var freeze *db.WriteFreeze
	defer func() {
		resultErr = errors.Join(resultErr, freeze.Close())
		if resultErr != nil {
			if work != "" && !retainFailure {
				resultErr = errors.Join(resultErr, removeCandidate(work))
			}
			candidate = DatabaseCandidate{}
		}
	}()
	if err := owner.AssertHeld(current.Config.Database, current.Config.NodeIdentityDirectory); err != nil {
		return candidate, err
	}
	plan, err := archive.VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256)
	if err != nil {
		return candidate, err
	}
	if plan.Target.Panel.Database != owner.Database || plan.Target.Panel.NodeIdentityDirectory != owner.IdentityDirectory {
		return candidate, ErrEvidence
	}
	freeze, err = db.FreezeForRecovery(ctx, current.Config.Database)
	if err != nil {
		return candidate, err
	}
	if err := current.CheckConfigurationKey(); err != nil {
		return candidate, ErrEvidence
	}
	material, err := archive.ReadMaterials(current.Config.NodeIdentityDirectory)
	if err != nil {
		return candidate, ErrEvidence
	}
	stored, err := archive.ReadMaterials(filepath.Join(prepared.Directory, "identity"))
	if err != nil || material.OriginalMaterials() != stored.OriginalMaterials() {
		return candidate, ErrEvidence
	}
	if work == "" {
		work, err = os.MkdirTemp(filepath.Dir(owner.Database), ".recovery-")
	} else {
		err = os.Mkdir(work, 0700)
	}
	if err != nil {
		return candidate, err
	}
	if err := writeExclusive(ctx, filepath.Join(work, ".panel-recovery-candidate"), []byte("proxy-control-recovery-candidate-v1\n")); err != nil {
		return candidate, err
	}
	for _, item := range plan.Files {
		if err := copyPinned(ctx, prepared.Directory, work, item.File); err != nil {
			return candidate, err
		}
	}
	evidenceDirectory := filepath.Join(work, "evidence")
	var evidence archive.Result
	var evidenceID string
	if preparation.Original != nil {
		evidence = preparation.Original.Result
		evidenceID = evidence.Manifest.ID
		if !validRecoveryID(evidenceID) || !validHash(evidence.SHA256) || evidence.Bytes < 1 || evidence.Bytes > archive.MaxArchiveBytes {
			return candidate, ErrEvidence
		}
		if err := copyPinned(ctx, preparation.Original.Directory, evidenceDirectory, archive.File{Name: evidenceID + ".pcb", Bytes: evidence.Bytes, SHA256: evidence.SHA256}); err != nil {
			return candidate, err
		}
	} else {
		evidenceID, err = id.New()
		if err != nil {
			return candidate, err
		}
		evidence, err = current.Create(ctx, evidenceDirectory, evidenceID, "local-restore", password)
		if err != nil {
			return candidate, err
		}
		if preparation.OnEvidence != nil {
			if err := preparation.OnEvidence(candidateEvidence{Directory: evidenceDirectory, Result: evidence}); err != nil {
				return candidate, err
			}
		}
	}
	databasePath := filepath.Join(work, "database", "panel.db")
	candidateDatabase, err := db.Open(ctx, databasePath)
	if err != nil {
		return candidate, err
	}
	defer candidateDatabase.Close()
	manifest := CandidateManifest{Version: 1, PreparedAt: time.Now().UTC().Format(time.RFC3339Nano), SourcePlanSHA256: prepared.ManifestSHA256, SnapshotID: plan.Source.ID, ArchiveSHA256: plan.ArchiveSHA256, EvidenceID: evidenceID, EvidenceSHA256: evidence.SHA256, Target: plan.Target}
	_, err = archive.Examine(ctx, evidenceDirectory, filepath.Join(evidenceDirectory, evidenceID+".pcb"), password, func(result archive.Result, evidenceWork string) error {
		if result.SHA256 != evidence.SHA256 {
			return ErrEvidence
		}
		if err := db.AttachRecoverySource(ctx, candidateDatabase, filepath.Join(evidenceWork, "database", "panel.db")); err != nil {
			return err
		}
		defer candidateDatabase.ExecContext(context.Background(), "DETACH DATABASE "+db.RecoverySchema)
		tx, err := candidateDatabase.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		snapshot, err := archive.ReadInventory(ctx, tx)
		if err != nil {
			return err
		}
		manifest.Snapshot = snapshot.Totals
		// Read the attached immutable source through an independent reader. All
		// modules below read the same authenticated evidence file.
		observed, err := archive.InventoryFromVerifiedDirectory(ctx, evidenceWork)
		if err != nil {
			return err
		}
		manifest.Current = observed.Totals
		if observed.Totals.PendingOperations != 0 || observed.Totals.BackupFileOperations != 0 {
			return fmt.Errorf("current execution remains unresolved: %w", ErrEvidence)
		}
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		for index, step := range []func() error{
			func() error { return (&auth.Repository{}).PrepareRestoreTx(ctx, tx) },
			func() error { return (&node.Repository{}).PrepareRestoreTx(ctx, tx, stamp) },
			func() error { return (&user.Repository{}).PrepareRestoreTx(ctx, tx) },
			func() error { return (&portal.Repository{}).PrepareRestoreTx(ctx, tx) },
			func() error { return (&subscription.Repository{}).PrepareRestoreTx(ctx, tx, stamp) },
			func() error { return (&importer.Store{}).PrepareRestoreTx(ctx, tx) },
			func() error { return (&proxy.Repository{}).PrepareClientRestoreTx(ctx, tx) },
			func() error { return (&access.Repository{}).PrepareRestoreTx(ctx, tx) },
			func() error { return (&deployment.Repository{}).PrepareRestoreTx(ctx, tx) },
			func() error { return (&system.Repository{}).PrepareRestoreTx(ctx, tx, stamp, plan.Source.ID) },
			func() error { return (&deployment.Repository{}).PrepareCoreUpdatesRestoreTx(ctx, tx) },
		} {
			if err := step(); err != nil {
				return fmt.Errorf("recovery module %d: %w", index, err)
			}
		}
		merged, err := archive.ReadInventory(ctx, tx)
		if err != nil {
			return err
		}
		manifest.Candidate = merged.Totals
		if merged.Totals.Sessions != 0 || merged.Totals.PendingOperations != 0 || merged.Totals.BackupFileOperations != 0 || merged.Totals.RunningBackupJobs != 0 {
			return fmt.Errorf("candidate retains transient operations: %w", ErrEvidence)
		}
		return tx.Commit()
	})
	if err != nil {
		return candidate, err
	}
	var busy, pages, checkpointed int
	if err := candidateDatabase.QueryRowContext(ctx, "PRAGMA main.wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil || busy != 0 {
		return candidate, errors.Join(ErrEvidence, err)
	}
	if err := candidateDatabase.Close(); err != nil {
		return candidate, err
	}
	if err := archive.ValidateMaterialSet(ctx, work); err != nil {
		return candidate, err
	}
	for _, item := range plan.Files {
		file, err := digest(ctx, filepath.Join(work, filepath.FromSlash(item.Name)), archive.MaxUnpackedBytes)
		if err != nil {
			return candidate, err
		}
		file.Name = item.Name
		manifest.Files = append(manifest.Files, archive.RestoreFile{File: file, Destination: item.Destination})
	}
	manifest.Files = append(manifest.Files, archive.RestoreFile{File: archive.File{Name: "evidence/" + evidenceID + ".pcb", Bytes: evidence.Bytes, SHA256: evidence.SHA256}})
	data, err := json.Marshal(manifest)
	if err != nil || len(data) > 1<<20 {
		return candidate, ErrEvidence
	}
	if err := owner.AssertHeld(current.Config.Database, current.Config.NodeIdentityDirectory); err != nil {
		return candidate, err
	}
	if err := writeExclusive(ctx, filepath.Join(work, "candidate.json"), data); err != nil {
		return candidate, err
	}
	hash := sha256.Sum256(data)
	candidate = DatabaseCandidate{Directory: work, ManifestSHA256: hex.EncodeToString(hash[:]), Manifest: manifest}
	if _, err := VerifyCandidate(ctx, candidate, prepared); err != nil {
		return DatabaseCandidate{}, fmt.Errorf("candidate verification: %w", err)
	}
	return candidate, nil
}

func copyPinned(ctx context.Context, source, destination string, item archive.File) error {
	if err := os.MkdirAll(filepath.Join(destination, filepath.Dir(filepath.FromSlash(item.Name))), 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	input, err := root.Open(filepath.FromSlash(item.Name))
	if err != nil {
		return err
	}
	defer input.Close()
	if err := filelock.CheckSingleLink(input); err != nil {
		return err
	}
	output, err := os.OpenFile(filepath.Join(destination, filepath.FromSlash(item.Name)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(output, hash), io.LimitReader(cancelReader{ctx, input}, item.Bytes+1))
	if err != nil {
		return err
	}
	if n != item.Bytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return ErrEvidence
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return output.Close()
}

func writeExclusive(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func digest(ctx context.Context, path string, limit int64) (archive.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return archive.File{}, ErrEvidence
	}
	file, err := os.Open(path)
	if err != nil {
		return archive.File{}, err
	}
	defer file.Close()
	if err := filelock.CheckSingleLink(file); err != nil {
		return archive.File{}, err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(cancelReader{ctx, file}, limit+1))
	if err != nil || n != info.Size() {
		return archive.File{}, errors.Join(ErrEvidence, err)
	}
	return archive.File{Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

type cancelReader struct {
	context context.Context
	reader  io.Reader
}

func (r cancelReader) Read(data []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func removeCandidate(path string) error {
	if !filepath.IsAbs(path) || len(filepath.Base(path)) <= len(".recovery-") || filepath.Base(path)[:len(".recovery-")] != ".recovery-" {
		return ErrEvidence
	}
	return os.RemoveAll(path)
}
