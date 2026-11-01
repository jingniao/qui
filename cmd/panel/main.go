// Go 1.24 and later open listening sockets as MPTCP by default on Linux; the
// remote root exploit for the kernel bug CVE-2026-98432 needs an MPTCP listener.

//go:debug multipathtcp=0
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/logging"
	"proxycontrol/internal/platform/panelhost"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/recovery"
	"proxycontrol/internal/platform/restoretasks"
	"proxycontrol/internal/platform/server"
	"proxycontrol/migrations"
	"proxycontrol/web"
)

var version = "dev"

func main() {
	os.Exit(exitStatus(run()))
}

// exitStatus maps a run outcome to the process status. Commands that already
// printed their result carry their own code and must not be logged as failures;
// everything else keeps the historical exit code 1.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var code exitCode
	if errors.As(err, &code) {
		return int(code)
	}
	slog.Error("panel failed", "error", err)
	return 1
}

// exitCode is a process status that a command reports instead of an error.
type exitCode int

func (c exitCode) Error() string {
	return fmt.Sprintf("exit status %d", int(c))
}

// Database compatibility verdicts that the update script distinguishes. They
// stay away from 1 (generic failure) and 2 (flag parsing) so an installed
// binary that predates this flag cannot be mistaken for a verdict.
const (
	exitDatabaseNewer   = exitCode(3)
	exitDatabaseForeign = exitCode(4)
	exitDatabaseDamaged = exitCode(5)
)

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runWithContext(ctx, os.Args[1:])
}

func runWithContext(ctx context.Context, args []string) (resultErr error) {
	flags := flag.NewFlagSet("proxy-panel", flag.ContinueOnError)
	configPath := flags.String("config", "", "YAML configuration file (default: local development)")
	inspectInstall := flags.Bool("inspect-restore-installation", false, "authenticate retained file installation journal and inspect original targets without recovery or startup")
	installReport := flags.String("restore-installation-report", "", "exclusive private JSON output for installation observation")
	check := flags.Bool("check-config", false, "validate configuration and exit")
	showVersion := flags.Bool("version", false, "show version")
	restorePath := flags.String("restore-backup", "", "restore a verified encrypted backup while the panel is stopped, then serve")
	passwordPath := flags.String("restore-password-file", "", "protected local file containing the backup password")
	setOwner := flags.Bool("set-owner-credentials", false, "replace the Owner username and password of a stopped panel, reading two lines from stdin")
	checkDatabase := flags.String("check-database", "", "report whether this binary can open a stopped panel database file (read-only, works on a private copy)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	// The update script asks the target binary first, so the check has to work
	// without a config, a service manager or any panel state.
	databaseRequested := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "check-database" {
			databaseRequested = true
		}
	})
	if databaseRequested {
		valid := *checkDatabase != ""
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "check-database" {
				valid = false
			}
		})
		if !valid {
			return errors.New("database compatibility check requires a file and no other flag")
		}
		return checkDatabaseFile(ctx, *checkDatabase)
	}
	installationRequested := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "inspect-restore-installation" || f.Name == "restore-installation-report" {
			installationRequested = true
		}
	})
	if installationRequested {
		valid := *inspectInstall && *configPath != "" && *installReport != ""
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "config" && f.Name != "inspect-restore-installation" && f.Name != "restore-installation-report" {
				valid = false
			}
		})
		if !valid {
			return errors.New("installation observation requires only config, inspect-restore-installation and restore-installation-report")
		}
		return inspectRestoreInstallation(ctx, *configPath, *installReport)
	}
	if *setOwner {
		valid := *configPath != ""
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "config" && f.Name != "set-owner-credentials" {
				valid = false
			}
		})
		if !valid {
			return errors.New("owner credential reset requires an explicit config and no other flag")
		}
		return setOwnerCredentials(ctx, *configPath, os.Stdin)
	}
	if (*restorePath == "") != (*passwordPath == "") {
		return errors.New("restore-backup and restore-password-file must be supplied together")
	}
	if *restorePath != "" && (*configPath == "" || *check || *showVersion) {
		return errors.New("restore requires an explicit config and cannot be combined with version or check-config")
	}
	if *showVersion {
		fmt.Println("proxy-panel", version)
		return nil
	}
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)
	// The one-command node install downloads the release the panel itself runs,
	// so the server has to know the build version. It is not part of the
	// configuration file, which the restore flow compares with the struct it
	// loaded from disk.
	server.Version = version
	c, err := config.LoadPanelPaths(*configPath)
	if err != nil {
		return err
	}
	if err := c.ValidateStructure(); err != nil {
		return err
	}
	if *check {
		if err := c.Validate(); err != nil {
			return err
		}
		logger.Info("configuration valid")
		return nil
	}
	var restorePassword string
	if *restorePath != "" {
		restorePassword, err = readRestorePassword(*passwordPath)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	c.Database, c.NodeIdentityDirectory = owner.Database, owner.IdentityDirectory
	if err := recovery.RecoverFiles(ctx, owner); err != nil {
		return err
	}
	// File recovery may restore the original configuration itself.
	c, err = config.LoadPanel(*configPath)
	if err != nil {
		return err
	}
	dbDirectory, err := filepath.EvalSymlinks(filepath.Dir(c.Database))
	if err != nil {
		return err
	}
	resolvedDB, err := filepath.Abs(filepath.Join(dbDirectory, filepath.Base(c.Database)))
	if err != nil {
		return err
	}
	resolvedIdentity, err := filepath.EvalSymlinks(c.NodeIdentityDirectory)
	if err != nil {
		return err
	}
	resolvedIdentity, err = filepath.Abs(resolvedIdentity)
	if err != nil {
		return err
	}
	if err := owner.AssertHeld(resolvedDB, resolvedIdentity); err != nil {
		return err
	}
	c.Database, c.NodeIdentityDirectory = owner.Database, owner.IdentityDirectory
	progressDirectory := filepath.Join(filepath.Dir(c.Database), "restore-progress")
	if _, statErr := os.Lstat(progressDirectory); statErr == nil {
		progressKey, keyErr := os.ReadFile(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
		if keyErr != nil {
			return keyErr
		}
		reconcileErr := (restoretasks.Store{Directory: progressDirectory, Key: progressKey}).ReconcileInterrupted(ctx)
		clear(progressKey)
		if reconcileErr != nil {
			return reconcileErr
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if *restorePath != "" {
		return restoreAndServe(ctx, owner, c, *configPath, *restorePath, restorePassword, logger)
	}
	return panelhost.Run(ctx, owner, c, *configPath, web.Files(), logger)
}

// checkDatabaseFile prints the report the update script parses and returns the
// verdict as the process status. The report is written before the status so a
// caller can always explain a refusal.
func checkDatabaseFile(ctx context.Context, path string) error {
	inspection := db.Inspect(ctx, path, migrations.Files)
	fmt.Printf("database: %s\n", inspection.Verdict)
	fmt.Printf("applied: %d\n", len(inspection.Applied))
	fmt.Printf("pending: %d\n", len(inspection.Pending))
	fmt.Printf("unknown: %s\n", nameList(inspection.Unknown))
	fmt.Printf("mismatched: %s\n", nameList(inspection.Mismatched))
	fmt.Printf("last: %s\n", lastName(inspection.Applied))
	fmt.Printf("integrity: %s\n", singleLine(inspection.Integrity))
	switch inspection.Verdict {
	case db.VerdictCompatible:
		return nil
	case db.VerdictNewer:
		return exitDatabaseNewer
	case db.VerdictForeign:
		return exitDatabaseForeign
	default:
		return exitDatabaseDamaged
	}
}

func nameList(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ",")
}

func lastName(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return names[len(names)-1]
}

// singleLine keeps an integrity_check message, which may span lines, inside the
// one output line the script expects.
func singleLine(message string) string {
	return strings.Join(strings.Fields(message), " ")
}
