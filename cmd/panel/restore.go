package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/recovery"
	"proxycontrol/internal/platform/restoretasks"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/web"
)

func readRestorePassword(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 514 {
		return "", errors.New("restore password file must be a bounded regular file")
	}
	if info.Mode().Perm()&0077 != 0 && os.PathSeparator != '\\' {
		return "", errors.New("restore password file must be accessible only to its owner")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("restore password file changed while opening")
	}
	if err := filelock.CheckSingleLink(file); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(file, 515))
	if err != nil {
		return "", err
	}
	defer clear(data)
	data = bytes.TrimSuffix(data, []byte("\n"))
	data = bytes.TrimSuffix(data, []byte("\r"))
	password := string(data)
	if err := archive.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}

func restoreAndServe(ctx context.Context, owner *panelstore.Ownership, c config.Panel, configurationPath, backupPath, password string, logger *slog.Logger) (resultErr error) {
	// A missing current database is a separate disaster recovery condition;
	// never fabricate zero usage/allowances by creating a fresh one here.
	info, err := os.Lstat(c.Database)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return recovery.ErrEvidence
	}
	configurationPath, err = filepath.Abs(configurationPath)
	if err != nil {
		return err
	}
	configurationPath, err = filepath.EvalSymlinks(configurationPath)
	if err != nil {
		return err
	}
	prepared, err := archive.PrepareRestore(ctx, filepath.Join(filepath.Dir(c.Database), "restore-staging"), backupPath, password, archive.RestoreTarget{Panel: c, ConfigurationPath: configurationPath})
	if err != nil {
		return err
	}
	preparedRetained := true
	defer func() {
		if preparedRetained {
			resultErr = errors.Join(resultErr, archive.DiscardPreparedRestore(context.Background(), prepared))
		}
	}()
	// Authentication precedes migrations or other current database operations.
	key, err := os.ReadFile(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		return recovery.ErrEvidence
	}
	configurationVault, err := vault.FromKey(key)
	progressKey := bytes.Clone(key)
	defer clear(progressKey)
	clear(key)
	if err != nil {
		return err
	}
	progress := restoretasks.Store{Directory: filepath.Join(filepath.Dir(c.Database), "restore-progress"), Key: progressKey}
	record, err := progress.Begin(ctx, prepared.Plan.Source.ID, prepared.Plan.ArchiveSHA256)
	if err != nil {
		return err
	}
	stage := "preparing"
	defer func() {
		if resultErr != nil {
			_, recordErr := progress.Advance(context.Background(), record.ID, stage, "failed", "restore_failed")
			resultErr = errors.Join(resultErr, recordErr)
		}
	}()
	database, err := db.Open(ctx, c.Database)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	source := archive.Source{Database: database, Config: c, ConfigurationVault: configurationVault}
	candidate, err := recovery.PrepareDatabase(ctx, owner, source, prepared, password)
	if err != nil {
		return err
	}
	candidateRetained := true
	defer func() {
		if candidateRetained {
			resultErr = errors.Join(resultErr, recovery.DiscardCandidate(context.Background(), candidate, prepared))
		}
	}()
	if _, err := progress.Advance(ctx, record.ID, "installing", "active", ""); err != nil {
		return err
	}
	stage = "installing"
	runtime, err := recovery.InstallAndPrepare(ctx, owner, source, candidate, prepared, password, web.Files(), logger)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, runtime.Close()) }()
	if _, err := progress.Advance(ctx, record.ID, "committed", "active", ""); err != nil {
		return err
	}
	stage = "committed"
	evidence, err := recovery.FinalizeCandidate(ctx, candidate, prepared)
	if err != nil {
		return err
	}
	candidateRetained = false
	if err := archive.DiscardPreparedRestore(ctx, prepared); err != nil {
		return err
	}
	preparedRetained = false
	if _, err := progress.Advance(ctx, record.ID, "cleaned", "active", ""); err != nil {
		return err
	}
	stage = "cleaned"
	logger.Info("encrypted pre-restore evidence retained", "path", evidence)
	logger.Info("restore committed; starting prepared panel", "backup_id", candidate.Manifest.SnapshotID)
	if _, err := progress.Advance(ctx, record.ID, "serving", "active", ""); err != nil {
		return err
	}
	stage = "serving"
	recordStartup, err := progress.StartupRecorder(ctx, record.ID)
	if err != nil {
		return err
	}
	if err := runtime.ConfigureStartupRecorder(recordStartup); err != nil {
		return err
	}
	if err := runtime.Serve(ctx); err != nil {
		return err
	}
	return progress.FinishServing(ctx, record.ID, nil)
}
