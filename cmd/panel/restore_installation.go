package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/recovery"
	"time"
)

func inspectRestoreInstallation(ctx context.Context, configurationPath, reportPath string) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// During an interrupted replacement, use the separately retained original
	// config path mapping. Load it without initializing DB, CA or server state.
	configuration, err := config.LoadPanel(configurationPath)
	if err != nil {
		return err
	}
	owner, err := panelstore.Try(configuration.Database, configuration.NodeIdentityDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	progress, err := recovery.InspectFileInstall(ctx, owner)
	if err != nil {
		return err
	}
	reportPath, err = filepath.Abs(reportPath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(reportPath))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("installation report requires an existing private directory")
	}
	report, err := os.OpenFile(reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		resultErr = errors.Join(resultErr, report.Close())
		if !committed {
			resultErr = errors.Join(resultErr, os.Remove(reportPath))
		}
	}()
	encoder := json.NewEncoder(report)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(progress); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := report.Sync(); err != nil {
		return err
	}
	committed = true
	return nil
}
