package main

import (
	"bytes"
	"context"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/panelstore"
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
