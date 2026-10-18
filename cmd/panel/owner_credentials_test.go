package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/panelstore"
)

const (
	ownerCredentialSetupToken    = "owner-credential-setup-token"
	ownerCredentialSetupPassword = "initial-owner-password-123"
	ownerCredentialNewPassword   = "replacement-owner-password-456"
	ownerCredentialViewerName    = "viewer-one"
	ownerCredentialViewerPass    = "viewer-password-123"
)

// ownerCredentialDatabase builds a stopped panel with one Owner, exactly like a
// panel that has already finished the web setup.
func ownerCredentialDatabase(t *testing.T) (config.Panel, string, *auth.Service, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	c, path := panelConfig(t, filepath.Join(root, "panel.db"), filepath.Join(root, "identity"))
	database, err := db.Open(context.Background(), c.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	systems := &system.Repository{Database: database}
	admins := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: systems, Settings: systems, SessionDuration: time.Hour}
	admins.SetSetupToken(ownerCredentialSetupToken, "")
	if _, _, err := admins.Setup(context.Background(), ownerCredentialSetupToken, "owner", ownerCredentialSetupPassword, "Owner credential test", "UTC", "local"); err != nil {
		t.Fatal(err)
	}
	return c, path, admins, database
}

func ownerRow(t *testing.T, database *sql.DB) (string, string) {
	t.Helper()
	var username, hash string
	if err := database.QueryRow("SELECT username,password_hash FROM admins WHERE role='Owner'").Scan(&username, &hash); err != nil {
		t.Fatal(err)
	}
	return username, hash
}

func ownerSessions(t *testing.T, database *sql.DB) int {
	t.Helper()
	var sessions int
	if err := database.QueryRow("SELECT count(*) FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE a.role='Owner'").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	return sessions
}

func ownerCredentialOperations(t *testing.T, database *sql.DB) int {
	t.Helper()
	var operations int
	if err := database.QueryRow("SELECT count(*) FROM admin_operations WHERE operator='system' AND action='终端重置 Owner 凭据' AND result='成功'").Scan(&operations); err != nil {
		t.Fatal(err)
	}
	return operations
}

func TestSetOwnerCredentialsCommandReplacesOwnerAndRevokesSessions(t *testing.T) {
	c, path, admins, database := ownerCredentialDatabase(t)
	// The web setup already created one session for the Owner.
	sessions := ownerSessions(t, database)
	if _, _, err := admins.Login(context.Background(), "owner", ownerCredentialSetupPassword, "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	if ownerSessions(t, database) != sessions+1 {
		t.Fatal("fixture did not create an Owner session")
	}
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.WriteString("renamed-owner\n" + ownerCredentialNewPassword + "\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	previousIn, previousOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdout
	defer func() {
		os.Stdin, os.Stdout = previousIn, previousOut
		stdin.Close()
		stdout.Close()
	}()
	if err := runWithContext(context.Background(), []string{"--config", path, "--set-owner-credentials"}); err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = previousIn, previousOut
	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if string(printed) != "owner credentials updated; sessions revoked\n" {
		t.Fatalf("unexpected output %q", printed)
	}
	if username, _ := ownerRow(t, database); username != "renamed-owner" {
		t.Fatal("username not replaced", username)
	}
	if ownerSessions(t, database) != 0 {
		t.Fatal("Owner session survived the credential reset")
	}
	if _, _, err := admins.Login(context.Background(), "owner", ownerCredentialSetupPassword, "192.0.2.11"); !errors.Is(err, auth.ErrCredentials) {
		t.Fatal("the previous password still authenticates", err)
	}
	if _, _, err := admins.Login(context.Background(), "renamed-owner", ownerCredentialNewPassword, "192.0.2.12"); err != nil {
		t.Fatal("the new password does not authenticate", err)
	}
	if ownerCredentialOperations(t, database) != 1 {
		t.Fatal("operation log entry missing")
	}
	// The command must release the panel ownership it took.
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal("credential reset retained ownership", err)
	}
	owner.Close()
}

func TestSetOwnerCredentialsKeepsUsernameOnEmptyLine(t *testing.T) {
	_, path, admins, database := ownerCredentialDatabase(t)
	if err := setOwnerCredentials(context.Background(), path, strings.NewReader("\n"+ownerCredentialNewPassword+"\n")); err != nil {
		t.Fatal(err)
	}
	if username, _ := ownerRow(t, database); username != "owner" {
		t.Fatal("empty username line changed the username", username)
	}
	if _, _, err := admins.Login(context.Background(), "owner", ownerCredentialNewPassword, "192.0.2.20"); err != nil {
		t.Fatal("the new password does not authenticate", err)
	}
	if _, _, err := admins.Login(context.Background(), "owner", ownerCredentialSetupPassword, "192.0.2.21"); !errors.Is(err, auth.ErrCredentials) {
		t.Fatal("the previous password still authenticates", err)
	}
}

func TestSetOwnerCredentialsRejectsShortPasswordAndTakenUsername(t *testing.T) {
	_, path, admins, database := ownerCredentialDatabase(t)
	_, before := ownerRow(t, database)
	if err := setOwnerCredentials(context.Background(), path, strings.NewReader("renamed-owner\nshort\n")); err == nil {
		t.Fatal("password shorter than the policy was accepted")
	}
	if username, hash := ownerRow(t, database); username != "owner" || hash != before {
		t.Fatal("rejected password changed the Owner row", username)
	}
	actor, err := admins.Repository.FindUsername(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admins.CreateAdmin(context.Background(), actor, ownerCredentialViewerName, "Viewer One", ownerCredentialViewerPass, "Viewer", "local"); err != nil {
		t.Fatal(err)
	}
	if err := setOwnerCredentials(context.Background(), path, strings.NewReader(ownerCredentialViewerName+"\n"+ownerCredentialNewPassword+"\n")); !errors.Is(err, auth.ErrConflict) {
		t.Fatal("a username taken by a Viewer was accepted", err)
	}
	if username, hash := ownerRow(t, database); username != "owner" || hash != before {
		t.Fatal("conflicting username changed the Owner row", username)
	}
	if _, err := admins.Repository.FindUsername(context.Background(), ownerCredentialViewerName); err != nil {
		t.Fatal("the Viewer account was changed", err)
	}
}

func TestSetOwnerCredentialsRequiresAnOwner(t *testing.T) {
	root := t.TempDir()
	c, path := panelConfig(t, filepath.Join(root, "panel.db"), filepath.Join(root, "identity"))
	database, err := db.Open(context.Background(), c.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := setOwnerCredentials(context.Background(), path, strings.NewReader("renamed-owner\n"+ownerCredentialNewPassword+"\n")); !errors.Is(err, auth.ErrNoOwner) {
		t.Fatal("a database without an Owner was accepted", err)
	}
}

func TestSetOwnerCredentialsRequiresAnExistingDatabase(t *testing.T) {
	root := t.TempDir()
	_, path := panelConfig(t, filepath.Join(root, "missing", "panel.db"), filepath.Join(root, "identity"))
	err := setOwnerCredentials(context.Background(), path, strings.NewReader("renamed-owner\n"+ownerCredentialNewPassword+"\n"))
	if err == nil || !strings.Contains(err.Error(), "panel database is missing") {
		t.Fatal("a missing database was not refused", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "missing", "panel.db")); !os.IsNotExist(err) {
		t.Fatal("the command created a database", err)
	}
}

func TestSetOwnerCredentialsRefusesWhilePanelIsRunning(t *testing.T) {
	c, path, _, database := ownerCredentialDatabase(t)
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	err = setOwnerCredentials(context.Background(), path, strings.NewReader("renamed-owner\n"+ownerCredentialNewPassword+"\n"))
	if err == nil || !strings.Contains(err.Error(), "the panel is running; stop proxy-panel.service first") {
		t.Fatal("a running panel did not refuse the reset", err)
	}
	if username, hash := ownerRow(t, database); username != "owner" || hash == "" {
		t.Fatal("refused reset changed the Owner row", username)
	}
}

func TestSetOwnerCredentialsFlagRejectsOtherFlagsAndMissingConfig(t *testing.T) {
	_, path, _, database := ownerCredentialDatabase(t)
	_, before := ownerRow(t, database)
	for _, args := range [][]string{
		{"--set-owner-credentials"},
		{"--set-owner-credentials", "--config", path, "--check-config"},
		{"--set-owner-credentials", "--config", path, "--version"},
		{"--set-owner-credentials", "--config", path, "--restore-backup", "backup.pcb", "--restore-password-file", "password.txt"},
		{"--set-owner-credentials", "--config", path, "positional"},
	} {
		if err := runWithContext(context.Background(), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if username, hash := ownerRow(t, database); username != "owner" || hash != before {
		t.Fatal("a rejected flag combination changed the Owner row", username)
	}
}
