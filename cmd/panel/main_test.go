package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/panelstore"
	"strings"
	"testing"
	"time"
)

func panelConfig(t *testing.T, database, identity string) (config.Panel, string) {
	t.Helper()
	c := config.DefaultPanel()
	c.Database, c.NodeIdentityDirectory = database, identity
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.HTTPAddress = listener.Addr().String()
	listener.Close()
	c.GRPCAddress = "127.0.0.1:0"
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "panel.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return c, path
}

func TestNonRuntimeCommandsDoNotCreateStateOrAcquireLocks(t *testing.T) {
	root := t.TempDir()
	c, path := panelConfig(t, filepath.Join(root, "data", "panel.db"), filepath.Join(root, "identity"))
	for _, args := range [][]string{{"--version"}, {"--config", path, "--check-config"}, {"--help"}} {
		if err := runWithContext(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("inspection created runtime state", entries, err)
	}
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := runWithContext(context.Background(), []string{"--config", path, "--check-config"}); err != nil {
		t.Fatal("configuration check requires ownership", err)
	}
}

func TestActualPanelProcessOwnsDatabaseAndIdentityUntilStop(t *testing.T) {
	for _, stop := range []string{"graceful", "kill"} {
		t.Run(stop, func(t *testing.T) {
			root := t.TempDir()
			c, path := panelConfig(t, filepath.Join(root, "data", "panel.db"), filepath.Join(root, "identity"))
			processCtx, stopProcess := context.WithTimeout(context.Background(), 30*time.Second)
			defer stopProcess()
			command := exec.CommandContext(processCtx, os.Args[0], "-test.run=^TestPanelProcessChild$")
			command.Env = append(os.Environ(), "PROXY_PANEL_TEST_CONFIG="+path)
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			command.Stdout, command.Stderr = &output, &output
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
			deadline := time.Now().Add(20 * time.Second)
			for {
				response, err := client.Get("http://" + c.HTTPAddress + "/healthz")
				if err == nil {
					response.Body.Close()
					if response.StatusCode == 200 {
						break
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("panel child did not become healthy")
				}
				time.Sleep(20 * time.Millisecond)
			}
			_, sameDB := panelConfig(t, c.Database, filepath.Join(root, "other-identity"))
			_, sameIdentity := panelConfig(t, filepath.Join(root, "other-data", "panel.db"), c.NodeIdentityDirectory)
			for _, candidate := range []string{sameDB, sameIdentity} {
				if err := runWithContext(context.Background(), []string{"--config", candidate}); !errors.Is(err, filelock.ErrBusy) {
					t.Fatal("second panel entered live persistent store", err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "other-data", "panel.db")); !os.IsNotExist(err) {
				t.Fatal("rejected panel initialized database", err)
			}
			if _, err := os.Stat(filepath.Join(root, "other-identity", "ca.key")); !os.IsNotExist(err) {
				t.Fatal("rejected panel initialized authority", err)
			}
			if stop == "kill" {
				if err := command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := io.WriteString(input, "stop\n"); err != nil {
					t.Fatal(err)
				}
				if err := input.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = command.Wait()
			finished = true
			if stop == "graceful" && err != nil {
				t.Fatal("graceful panel shutdown failed", err, output.String())
			}
			owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
			if err != nil {
				t.Fatal("terminated panel still owns store", err)
			}
			owner.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			healthy := make(chan bool, 1)
			go func() {
				for ctx.Err() == nil {
					response, err := client.Get("http://" + c.HTTPAddress + "/healthz")
					if err == nil {
						response.Body.Close()
						if response.StatusCode == 200 {
							healthy <- true
							cancel()
							return
						}
					}
					time.Sleep(20 * time.Millisecond)
				}
				healthy <- false
			}()
			if err := runWithContext(ctx, []string{"--config", path}); err != nil {
				t.Fatal("panel restart after termination failed", err)
			}
			if !<-healthy {
				t.Fatal("restarted panel never became healthy")
			}
		})
	}
}

func TestPanelProcessChild(t *testing.T) {
	path := os.Getenv("PROXY_PANEL_TEST_CONFIG")
	if path == "" {
		t.Skip("subprocess only")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { var byte [1]byte; os.Stdin.Read(byte[:]); cancel() }()
	if err := runWithContext(ctx, []string{"--config", path}); err != nil {
		t.Fatal(err)
	}
}

func TestStartupFailureReleasesBothPersistentDirectories(t *testing.T) {
	for _, fault := range []string{"database", "authority", "listener"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			c, path := panelConfig(t, filepath.Join(root, "data", "panel.db"), filepath.Join(root, "identity"))
			switch fault {
			case "database":
				if err := os.MkdirAll(filepath.Dir(c.Database), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(c.Database, bytes.Repeat([]byte("broken"), 100), 0600); err != nil {
					t.Fatal(err)
				}
			case "authority":
				if err := os.MkdirAll(c.NodeIdentityDirectory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(c.NodeIdentityDirectory, "ca.key"), []byte("incomplete"), 0600); err != nil {
					t.Fatal(err)
				}
			case "listener":
				listener, err := net.Listen("tcp", c.HTTPAddress)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := runWithContext(ctx, []string{"--config", path}); err == nil {
				t.Fatal("startup fault did not reject service", fault)
			}
			owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
			if err != nil {
				t.Fatal("startup fault leaked directory ownership", fault, err)
			}
			owner.Close()
		})
	}
}

func TestCorruptRestoreJournalStopsPanelBeforeCreatingDatabaseOrCA(t *testing.T) {
	root := t.TempDir()
	c, path := panelConfig(t, filepath.Join(root, "data", "panel.db"), filepath.Join(root, "identity"))
	if err := os.MkdirAll(filepath.Dir(c.Database), 0700); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(filepath.Dir(c.Database), ".panel-restore.json")
	if err := os.WriteFile(journal, []byte(`{"untrusted":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runWithContext(context.Background(), []string{"--config", path}); err == nil {
		t.Fatal("panel started over unverified restore journal")
	}
	for _, candidate := range []string{c.Database, filepath.Join(c.NodeIdentityDirectory, "ca.key"), filepath.Join(c.NodeIdentityDirectory, "configuration.key")} {
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			t.Fatal("panel initialized state before restore recovery", candidate, err)
		}
	}
	data, err := os.ReadFile(journal)
	if err != nil || string(data) != `{"untrusted":true}` {
		t.Fatal("unverified restore journal deleted or changed", err)
	}
}

// captureStdout runs fn with its report redirected to a pipe; the lines are a
// contract the update script parses, so tests read them verbatim.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()
	defer func() {
		os.Stdout = previous
		reader.Close()
	}()
	runErr := fn()
	writer.Close()
	return <-done, runErr
}

// quietExitStatus maps an outcome while muting the failure log a refused
// database check must not print.
func quietExitStatus(t *testing.T, err error) int {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previous)
	return exitStatus(err)
}

func stoppedDatabase(t *testing.T, path string) {
	t.Helper()
	database, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseCompatibilityCheckCreatesNoStateOrLocks(t *testing.T) {
	root := t.TempDir()
	c, path := panelConfig(t, filepath.Join(root, "data", "panel.db"), filepath.Join(root, "identity"))
	// The inspected file lives outside the panel directories: only a private
	// copy of it may ever be touched.
	source := filepath.Join(t.TempDir(), "stopped.db")
	stoppedDatabase(t, source)
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := captureStdout(t, func() error {
		return runWithContext(context.Background(), []string{"--check-database", source})
	})
	if runErr != nil {
		t.Fatalf("compatible database rejected: %v", runErr)
	}
	if !strings.Contains(output, "database: compatible\n") {
		t.Fatalf("unexpected report:\n%s", output)
	}
	if after, err := os.ReadFile(source); err != nil || !bytes.Equal(before, after) {
		t.Fatal("check modified the inspected database", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("database check created runtime state", entries, err)
	}
	// The panel directories are still free, so the check neither loaded a
	// configuration nor kept a lock behind.
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal("database check left the panel directories locked", err)
	}
	owner.Close()
	if err := runWithContext(context.Background(), []string{"--config", path, "--check-config"}); err != nil {
		t.Fatal("database check left the configuration unusable", err)
	}
}

func TestDatabaseCompatibilityCheckRejectsOtherFlags(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "garbage.db")
	if err := os.WriteFile(garbage, bytes.Repeat([]byte("broken"), 100), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--check-database", garbage, "--version"},
		{"--check-database", garbage, "--config", ""},
		{"--check-database", ""},
	} {
		err := runWithContext(context.Background(), args)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
		var code exitCode
		if errors.As(err, &code) {
			t.Fatalf("%v reported a verdict instead of a usage error: %v", args, err)
		}
		if status := quietExitStatus(t, err); status != 1 {
			t.Fatalf("%v exited %d, want 1", args, status)
		}
	}
}

func TestDatabaseCompatibilityCheckExitCodes(t *testing.T) {
	ctx := context.Background()
	compatible := filepath.Join(t.TempDir(), "stopped.db")
	stoppedDatabase(t, compatible)
	output, err := captureStdout(t, func() error {
		return runWithContext(ctx, []string{"--check-database", compatible})
	})
	if status := exitStatus(err); status != 0 {
		t.Fatalf("compatible database exited %d: %v", status, err)
	}
	for _, line := range []string{"database: compatible\n", "pending: 0\n", "unknown: -\n", "mismatched: -\n", "integrity: ok\n"} {
		if !strings.Contains(output, line) {
			t.Fatalf("report lacks %q:\n%s", line, output)
		}
	}
	if strings.Contains(output, "last: -\n") || !strings.Contains(output, "applied: ") {
		t.Fatalf("report lacks the applied migrations:\n%s", output)
	}
	garbage := filepath.Join(t.TempDir(), "garbage.db")
	if err := os.WriteFile(garbage, bytes.Repeat([]byte("broken"), 100), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = captureStdout(t, func() error {
		return runWithContext(ctx, []string{"--check-database", garbage})
	})
	if status := exitStatus(err); status != 5 {
		t.Fatalf("damaged database exited %d: %v", status, err)
	}
	if !strings.Contains(output, "database: damaged\n") {
		t.Fatalf("unexpected report:\n%s", output)
	}
}

func TestDatabaseCompatibilityCheckReportsNewerDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "newer.db")
	stoppedDatabase(t, path)
	uri, err := db.URI(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	// A row for a migration this binary does not embed is exactly what a panel
	// of a later version leaves behind.
	if _, err := database.Exec("INSERT INTO schema_migrations(version, checksum, applied_at) VALUES('9999_future.sql', 'x', '2026-10-23T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	output, runErr := captureStdout(t, func() error {
		return runWithContext(ctx, []string{"--check-database", path})
	})
	if status := exitStatus(runErr); status != 3 {
		t.Fatalf("newer database exited %d: %v", status, runErr)
	}
	for _, line := range []string{"database: newer\n", "unknown: 9999_future.sql\n", "last: 9999_future.sql\n"} {
		if !strings.Contains(output, line) {
			t.Fatalf("report lacks %q:\n%s", line, output)
		}
	}
}

func TestDatabaseCompatibilityCheckReportsForeignDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreign.db")
	uri, err := db.URI(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE unrelated(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	output, runErr := captureStdout(t, func() error {
		return runWithContext(ctx, []string{"--check-database", path})
	})
	if status := exitStatus(runErr); status != 4 {
		t.Fatalf("foreign database exited %d: %v", status, runErr)
	}
	if !strings.Contains(output, "database: foreign\n") {
		t.Fatalf("unexpected report:\n%s", output)
	}
}
