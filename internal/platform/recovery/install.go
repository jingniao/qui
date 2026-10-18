package recovery

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/server"
	"proxycontrol/web"
	"reflect"
	"strings"
)

// InstallCandidate is an offline operation. All HTTP/gRPC/background writers
// must be stopped. It rechecks evidence under a SQLite write freeze, closes
// the owned database, installs files, verifies target configuration, and commits
// their journal. Starting services and accepting new writes is a later step.
func InstallCandidate(ctx context.Context, owner *panelstore.Ownership, current archive.Source, candidate DatabaseCandidate, prepared archive.PreparedRestore, password string) error {
	return installCandidate(ctx, owner, current, candidate, prepared, password, nil)
}

func installCandidate(ctx context.Context, owner *panelstore.Ownership, current archive.Source, candidate DatabaseCandidate, prepared archive.PreparedRestore, password string, checkpoint func(string)) (resultErr error) {
	runtime, err := installAndPrepare(ctx, owner, current, candidate, prepared, password, web.Files(), nil, checkpoint)
	if runtime != nil {
		err = errors.Join(err, runtime.Close())
	}
	return err
}

// InstallAndPrepare validates and holds both listening sockets before journal
// commitment. Serve on the returned reservation may start only after success;
// ownership must remain held through that service's entire lifetime.
func InstallAndPrepare(ctx context.Context, owner *panelstore.Ownership, current archive.Source, candidate DatabaseCandidate, prepared archive.PreparedRestore, password string, files fs.FS, logger *slog.Logger) (*server.PreparedRestore, error) {
	return installAndPrepare(ctx, owner, current, candidate, prepared, password, files, logger, nil)
}

func installAndPrepare(ctx context.Context, owner *panelstore.Ownership, current archive.Source, candidate DatabaseCandidate, prepared archive.PreparedRestore, password string, ui fs.FS, logger *slog.Logger, checkpoint func(string)) (runtime *server.PreparedRestore, resultErr error) {
	defer func() {
		if resultErr != nil && runtime != nil {
			resultErr = errors.Join(resultErr, runtime.Close())
			runtime = nil
		}
	}()
	if err := owner.AssertHeld(current.Config.Database, current.Config.NodeIdentityDirectory); err != nil {
		return nil, err
	}
	manifest, err := VerifyCandidate(ctx, candidate, prepared)
	if err != nil {
		return nil, err
	}
	if manifest.Target.Panel.Database != owner.Database || manifest.Target.Panel.NodeIdentityDirectory != owner.IdentityDirectory {
		return nil, ErrEvidence
	}
	ordered, err := archive.OrderRestoreFiles(manifest.Files)
	if err != nil {
		return nil, err
	}
	freeze, err := db.FreezeForRecovery(ctx, current.Config.Database)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, freeze.Close()) }()
	evidencePath := filepath.Join(candidate.Directory, "evidence", manifest.EvidenceID+".pcb")
	freshDirectory, err := os.MkdirTemp(filepath.Dir(owner.Database), ".restore-check-")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(freshDirectory)) }()
	evidence, err := archive.Inspect(ctx, freshDirectory, evidencePath, password)
	if err != nil {
		return nil, err
	}
	if evidence.SHA256 != manifest.EvidenceSHA256 {
		return nil, ErrEvidence
	}
	freshID, err := id.New()
	if err != nil {
		return nil, err
	}
	fresh, err := current.Create(ctx, freshDirectory, freshID, "local-restore-check", password)
	if err != nil {
		return nil, err
	}
	if !sameFiles(evidence.Manifest.Files, fresh.Manifest.Files) {
		return nil, ErrEvidence
	}
	if err := freeze.Close(); err != nil {
		return nil, err
	}
	// Checkpoint all committed WAL bytes before preserving the physical DB.
	var busy, pages, checkpointed int
	if err := current.Database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil || busy != 0 {
		return nil, errors.Join(ErrEvidence, err)
	}
	if err := current.Database.Close(); err != nil {
		return nil, err
	}
	files := []archive.RestoreFile{}
	sources := map[string]string{}
	for _, item := range ordered {
		if item.Destination == "" {
			continue
		}
		files = append(files, item)
		sources[item.Name] = filepath.Join(candidate.Directory, filepath.FromSlash(item.Name))
	}
	// Stale rules removed by the selected snapshot must disappear as well.
	wanted := map[string]bool{}
	for _, item := range files {
		wanted[item.Destination] = true
	}
	for _, item := range fresh.Manifest.Files {
		if !strings.HasPrefix(item.Name, "rulesets/") {
			continue
		}
		destination := filepath.Join(filepath.Dir(owner.Database), filepath.FromSlash(item.Name))
		if !wanted[destination] {
			files = append(files, archive.RestoreFile{File: archive.File{Name: item.Name}, Destination: destination})
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := owner.Database + suffix
		if _, err := os.Lstat(path); err == nil {
			files = append(files, archive.RestoreFile{File: archive.File{Name: "sidecar" + suffix}, Destination: path})
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	err = installFilesWithCleanup(ctx, owner, files, sources, func() error {
		loaded, err := config.LoadPanel(manifest.Target.ConfigurationPath)
		if err != nil || !reflect.DeepEqual(loaded, manifest.Target.Panel) {
			return ErrEvidence
		}
		for _, item := range files {
			if sources[item.Name] == "" {
				if _, err := os.Lstat(item.Destination); !os.IsNotExist(err) {
					return ErrFileRecovery
				}
				continue
			}
			actual, err := digest(ctx, item.Destination, item.Bytes)
			if err != nil || actual.Bytes != item.Bytes || actual.SHA256 != item.SHA256 {
				return ErrFileRecovery
			}
		}
		runtime, err = server.PrepareRestore(ctx, loaded, ui, logger)
		if err == nil {
			err = runtime.BindOwnership(owner)
		}
		return err
	}, checkpoint, func() error {
		if runtime != nil {
			return runtime.Close()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

func sameFiles(left, right []archive.File) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
