package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/panelstore"
)

// The terminal menu pipes exactly two lines into this command: the new username
// (an empty line keeps the current one) and the new password. Both lines are
// read under a bound so a mistyped input cannot exhaust memory, and the password
// is never taken from argv.
const (
	maxOwnerCredentialLine  = 4096
	maxOwnerCredentialInput = 64 << 10
)

// ownerCredentialAction names the operation log entry of the offline reset. The
// target stays the account, not the chosen username, so the entry is complete
// even when the username line was empty.
const ownerCredentialAction = "终端重置 Owner 凭据"

func readOwnerCredentialLine(reader *bufio.Reader, name string) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if len(line) > maxOwnerCredentialLine {
		return "", fmt.Errorf("the %s line is too long", name)
	}
	if errors.Is(err, io.EOF) && line == "" {
		return "", fmt.Errorf("standard input ended before the %s line", name)
	}
	return line, nil
}

func readOwnerCredentials(input io.Reader) (string, string, error) {
	reader := bufio.NewReader(io.LimitReader(input, maxOwnerCredentialInput))
	username, err := readOwnerCredentialLine(reader, "username")
	if err != nil {
		return "", "", err
	}
	password, err := readOwnerCredentialLine(reader, "password")
	if err != nil {
		return "", "", err
	}
	return username, password, nil
}

// setOwnerCredentials replaces the Owner username and password of a stopped
// panel. The command takes the same lifetime ownership the server takes, so a
// running proxy-panel.service refuses the change instead of racing the live
// process. An existing database is opened with the same helper the server and
// the offline restore use, so pending migrations are applied by db.Open; a
// missing database is refused rather than created.
func setOwnerCredentials(ctx context.Context, configurationPath string, input io.Reader) (resultErr error) {
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
	// A missing database is not a credential problem: never fabricate an empty
	// one here, the same rule the offline restore follows.
	if info, err := os.Lstat(owner.Database); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("panel database is missing; start the panel once to initialize it")
	}
	database, err := db.Open(ctx, owner.Database)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	username, password, err := readOwnerCredentials(input)
	if err != nil {
		return err
	}
	if err := (&auth.Repository{Database: database}).SetOwnerCredentials(ctx, username, password); err != nil {
		return err
	}
	if err := (&system.Repository{Database: database}).Record(ctx, "system", ownerCredentialAction, "Owner", "成功", ""); err != nil {
		return fmt.Errorf("owner credentials updated but the operation log entry failed: %w", err)
	}
	fmt.Println("owner credentials updated; sessions revoked")
	return nil
}
