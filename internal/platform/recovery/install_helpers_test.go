package recovery

import (
	"context"
	"errors"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/web"
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

// InstallFiles replaces a fully prepared file set while retaining originals.
// Callers must close every database/service handle and validate evidence
// freshness first. Validation must not run proxy control or accept writes.
// A failure or process exit before commitment restores the original file set.
func installFiles(ctx context.Context, owner *panelstore.Ownership, files []archive.RestoreFile, sources map[string]string, validate func() error, checkpoint func(string)) (resultErr error) {
	return installFilesWithCleanup(ctx, owner, files, sources, validate, checkpoint, nil)
}
