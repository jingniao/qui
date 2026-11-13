package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/panelstore"
)

// adminEntryActor is the operator recorded for the offline reset.
const adminEntryActor = "root (qui)"

// adminEntry shows or resets the admin login path of a stopped panel. Like the
// Owner credential reset it takes the panel's lifetime ownership, so a running
// proxy-panel.service refuses it, and it never creates a missing database.
func adminEntry(ctx context.Context, configurationPath, action string, output io.Writer) (resultErr error) {
	if action != "show" && action != "reset" {
		return errors.New("admin-entry requires show or reset")
	}
	configuration, err := config.LoadPanelPaths(configurationPath)
	if err != nil {
		return err
	}
	if err := configuration.ValidateStructure(); err != nil {
		return err
	}
	owner, err := panelstore.Try(configuration.Database, configuration.NodeIdentityDirectory)
	if err != nil {
		if errors.Is(err, filelock.ErrBusy) {
			return errors.New("the panel is running; stop proxy-panel.service first")
		}
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	if info, err := os.Lstat(owner.Database); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("panel database is missing; start the panel once to initialize it")
	}
	database, err := db.Open(ctx, owner.Database)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	repository := &system.Repository{Database: database}
	if action == "reset" {
		changed, err := repository.ResetAdminEntry(ctx, adminEntryActor)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(output, "admin login path reset to %s\n", system.DefaultAdminEntry)
		} else {
			fmt.Fprintf(output, "admin login path already %s\n", system.DefaultAdminEntry)
		}
		return nil
	}
	entry, err := repository.ReadAdminEntry(ctx)
	if errors.Is(err, system.ErrAdminEntryInvalid) {
		fmt.Fprintln(output, "admin login path is invalid; reset it with --admin-entry reset")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "admin login path: %s\n", entry.Path)
	return nil
}
