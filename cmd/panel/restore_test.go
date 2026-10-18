package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/restoretasks"
	"proxycontrol/internal/platform/vault"
	"strings"
	"testing"
	"time"
)

const cliPassword = "local-restore-test-password"

func restoreCLISetup(t *testing.T) (config.Panel, string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	c, path := panelConfig(t, filepath.Join(root, "panel.db"), filepath.Join(root, "identity"))
	database, err := db.Open(context.Background(), c.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	authority, err := pki.LoadOrCreate(c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.DevelopmentServer(); err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	systems := &system.Repository{Database: database}
	admins := &auth.Service{Repository: &auth.Repository{Database: database}, Audit: systems, Settings: systems, SessionDuration: time.Hour}
	admins.SetSetupToken("cli-setup-token", "")
	if _, _, err := admins.Setup(context.Background(), "cli-setup-token", "owner", "test-owner-password", "CLI test", "UTC", "local"); err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: database, Vault: key}
	u, err := users.Save(context.Background(), "", user.Input{Username: "cli-user", DisplayName: "Snapshot display", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "backups")
	result, err := (archive.Source{Database: database, Config: c, ConfigurationVault: key}).Create(context.Background(), directory, "cli_snapshot_0001", "owner", cliPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE users SET display_name='Current display',used_traffic=900 WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	passwordPath := filepath.Join(root, "password.txt")
	if err := os.WriteFile(passwordPath, []byte(cliPassword+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return c, path, filepath.Join(directory, result.Manifest.ID+".pcb"), passwordPath, u.ID
}

func TestRestoreCommandAuthenticatesBeforeCurrentDatabaseChanges(t *testing.T) {
	c, path, backup, password, userID := restoreCLISetup(t)
	if err := os.WriteFile(password, []byte("incorrect-passphrase"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runWithContext(context.Background(), []string{"--config", path, "--restore-backup", backup, "--restore-password-file", password}); err == nil {
		t.Fatal("incorrect backup password restored database")
	}
	database, err := db.Open(context.Background(), c.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var display string
	var used int64
	if err := database.QueryRow("SELECT display_name,used_traffic FROM users WHERE id=?", userID).Scan(&display, &used); err != nil || display != "Current display" || used != 900 {
		t.Fatal("failed restore altered business state", display, used, err)
	}
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal("failed command retained ownership", err)
	}
	owner.Close()
}

func TestRestoreCommandRequiresStoppedPanelAndCompleteArguments(t *testing.T) {
	c, path, backup, password, _ := restoreCLISetup(t)
	for _, args := range [][]string{{"--restore-backup", backup}, {"--restore-password-file", password}, {"--restore-backup", backup, "--restore-password-file", password}, {"--config", path, "--restore-backup", backup, "--restore-password-file", password, "--check-config"}} {
		if err := runWithContext(context.Background(), args); err == nil {
			t.Fatal("incomplete or conflicting restore options accepted", args)
		}
	}
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := runWithContext(context.Background(), []string{"--config", path, "--restore-backup", backup, "--restore-password-file", password}); !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("restore entered live panel store", err)
	}
}

func TestRestoreCommandStartsServiceAndCleansPlaintextMaterials(t *testing.T) {
	c, path, backup, password, userID := restoreCLISetup(t)
	commandCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binary := os.Getenv("PANEL_BINARY")
	var command *exec.Cmd
	if binary != "" {
		command = exec.CommandContext(commandCtx, binary, "--config", path, "--restore-backup", backup, "--restore-password-file", password)
	} else {
		command = exec.CommandContext(commandCtx, os.Args[0], "-test.run=^TestRestoreCLIChild$")
		command.Env = append(os.Environ(), "PROXY_RESTORE_CLI_CONFIG="+path, "PROXY_RESTORE_CLI_BACKUP="+backup, "PROXY_RESTORE_CLI_PASSWORD_FILE="+password)
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			command.Process.Kill()
			command.Wait()
		}
	}()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(60 * time.Second)
	for {
		response, err := client.Get("http://" + c.HTTPAddress + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			command.Process.Kill()
			command.Wait()
			finished = true
			t.Fatal("restore CLI did not start healthy service", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, pattern := range []string{filepath.Join(filepath.Dir(c.Database), ".recovery-*"), filepath.Join(filepath.Dir(c.Database), "restore-staging", ".restore-*")} {
		paths, _ := filepath.Glob(pattern)
		if len(paths) != 0 {
			t.Fatal("healthy restored service retained plaintext preparation", paths)
		}
	}
	if binary != "" {
		if err := command.Process.Kill(); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := io.WriteString(input, "stop\n"); err != nil {
			t.Fatal(err)
		}
	}
	input.Close()
	if err := command.Wait(); err != nil && binary == "" {
		t.Fatal("restore CLI failed", err, output.String())
	}
	finished = true
	if bytes.Contains(output.Bytes(), []byte(cliPassword)) {
		t.Fatal("backup password entered command output")
	}
	progressKey, err := os.ReadFile(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(progressKey)
	progress := restoretasks.Store{Directory: filepath.Join(filepath.Dir(c.Database), "restore-progress"), Key: progressKey}
	records, err := progress.List(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatal("CLI progress missing", records, err)
	}
	if binary == "" {
		if records[0].Status != "stopped" || records[0].LastStage != "stopped" {
			t.Fatal("graceful CLI progress not terminal", records)
		}
	} else {
		if records[0].Status != "active" || records[0].LastStage != "serving" {
			t.Fatal("killed CLI last stage lost", records)
		}
		restart := exec.CommandContext(commandCtx, binary, "--config", path)
		if err := restart.Start(); err != nil {
			t.Fatal(err)
		}
		restartFinished := false
		defer func() {
			if !restartFinished {
				restart.Process.Kill()
				restart.Wait()
			}
		}()
		restartDeadline := time.Now().Add(60 * time.Second)
		for {
			response, err := client.Get("http://" + c.HTTPAddress + "/healthz")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					break
				}
			}
			if time.Now().After(restartDeadline) {
				t.Fatal("normal restart did not reconcile restore progress")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := restart.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		restart.Wait()
		restartFinished = true
		records, err = progress.List(context.Background())
		if err != nil || records[0].Status != "interrupted" || records[0].LastStage != "serving" {
			t.Fatal("startup fabricated completed or rolled back outcome", records, err)
		}
	}
	database, err := db.Open(context.Background(), c.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var display string
	var used int64
	if err := database.QueryRow("SELECT display_name,used_traffic FROM users WHERE id=?", userID).Scan(&display, &used); err != nil || display != "Snapshot display" || used != 900 {
		t.Fatal("CLI lost retained consumption or snapshot draft", display, used, err)
	}
	for _, pattern := range []string{filepath.Join(filepath.Dir(c.Database), ".recovery-*"), filepath.Join(filepath.Dir(c.Database), "restore-staging", ".restore-*")} {
		paths, _ := filepath.Glob(pattern)
		if len(paths) != 0 {
			t.Fatal("CLI left plaintext recovery material", paths)
		}
	}
	evidence, _ := filepath.Glob(filepath.Join(filepath.Dir(c.Database), "restore-evidence", "*.pcb"))
	if len(evidence) != 1 {
		t.Fatal("encrypted pre-restore evidence not retained", evidence)
	}
	if _, err := archive.Inspect(context.Background(), t.TempDir(), evidence[0], cliPassword); err != nil {
		t.Fatal("retained evidence is unusable", err)
	}
}

func TestRestorePasswordFileRejectsInvalidOrAliasedMaterial(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "password.txt")
	for _, data := range []string{"short", strings.Repeat("x", 515), "invalid\xffpassword"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRestorePassword(path); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	if err := os.WriteFile(path, []byte(cliPassword), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.txt")
	if err := os.Link(path, alias); err == nil {
		if _, err := readRestorePassword(path); err == nil {
			t.Fatal("hardlinked password file accepted")
		}
		os.Remove(alias)
	}
	if password, err := readRestorePassword(path); err != nil || password != cliPassword {
		t.Fatal("valid private file rejected", err)
	}
}

func TestRestoreCLIChild(t *testing.T) {
	path := os.Getenv("PROXY_RESTORE_CLI_CONFIG")
	if path == "" {
		t.Skip("subprocess only")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { var byte [1]byte; os.Stdin.Read(byte[:]); cancel() }()
	if err := runWithContext(ctx, []string{"--config", path, "--restore-backup", os.Getenv("PROXY_RESTORE_CLI_BACKUP"), "--restore-password-file", os.Getenv("PROXY_RESTORE_CLI_PASSWORD_FILE")}); err != nil {
		t.Fatal(err)
	}
}
