package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"gopkg.in/yaml.v3"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/vault"
	"testing"
	"time"
)

func fileFixture(t *testing.T) (*panelstore.Ownership, []archive.RestoreFile, map[string]string) {
	t.Helper()
	root := t.TempDir()
	owner, err := panelstore.Try(filepath.Join(root, "panel.db"), filepath.Join(root, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	if err := os.WriteFile(filepath.Join(owner.IdentityDirectory, "configuration.key"), bytes.Repeat([]byte{7}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	files, sources := simpleFiles(t, root)
	return owner, files, sources
}
func simpleFiles(t *testing.T, root string) ([]archive.RestoreFile, map[string]string) {
	t.Helper()
	sourcesRoot := filepath.Join(root, "sources")
	if err := os.MkdirAll(sourcesRoot, 0700); err != nil {
		t.Fatal(err)
	}
	files := []archive.RestoreFile{}
	sources := map[string]string{}
	for _, name := range []string{"database", "configuration", "new-nested", "removed"} {
		path := filepath.Join(root, name)
		if name == "new-nested" {
			path = filepath.Join(root, "newdir", "file")
		}
		if name != "new-nested" {
			if err := os.WriteFile(path, []byte("old-"+name), 0600); err != nil {
				t.Fatal(err)
			}
		}
		item := archive.RestoreFile{File: archive.File{Name: name}, Destination: path}
		if name != "removed" {
			data := []byte("new-" + name)
			hash := sha256.Sum256(data)
			item.Bytes = int64(len(data))
			item.SHA256 = hex.EncodeToString(hash[:])
			source := filepath.Join(sourcesRoot, name)
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			sources[name] = source
		}
		files = append(files, item)
	}
	return files, sources
}

func TestFileTransactionCommitAndValidationFailureRollback(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[failure], func(t *testing.T) {
			owner, files, sources := fileFixture(t)
			expected := errors.New("startup validation failed")
			err := installFiles(context.Background(), owner, files, sources, func() error {
				if failure {
					return expected
				}
				return nil
			}, nil)
			if failure && !errors.Is(err, expected) || !failure && err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				data, err := os.ReadFile(file.Destination)
				missing := failure && file.Name == "new-nested" || !failure && file.Name == "removed"
				if missing {
					if !os.IsNotExist(err) {
						t.Fatal("transaction did not restore absent file state", file.Name, err)
					}
					continue
				}
				prefix := "new-"
				if failure {
					prefix = "old-"
				}
				if err != nil || string(data) != prefix+file.Name {
					t.Fatal("wrong target after file transaction", file.Name, string(data), err)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(owner.Database), journalName)); !os.IsNotExist(err) {
				t.Fatal("completed transaction left active journal", err)
			}
		})
	}
}

func TestActualProcessExitDuringFileReplacementRecoversOriginalsOrCommittedFiles(t *testing.T) {
	for _, point := range []string{"published", "installed-0", "installed-1", "installed-2", "installed-3", "validated", "committed"} {
		t.Run(point, func(t *testing.T) {
			owner, files, _ := fileFixture(t)
			root := filepath.Dir(owner.Database)
			owner.Close()
			commandCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(commandCtx, os.Args[0], "-test.run=^TestFileTransactionChild$")
			command.Env = append(os.Environ(), "PROXY_FILE_TX_ROOT="+root, "PROXY_FILE_TX_EXIT="+point)
			output, err := command.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 77 {
				t.Fatal("child did not stop at requested checkpoint", point, err, string(output))
			}
			reopened, err := panelstore.Try(owner.Database, owner.IdentityDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if err := RecoverFiles(context.Background(), reopened); err != nil {
				t.Fatal("interrupted transaction cannot recover", point, err)
			}
			for _, file := range files {
				data, err := os.ReadFile(file.Destination)
				committed := point == "committed"
				missing := !committed && file.Name == "new-nested" || committed && file.Name == "removed"
				if missing {
					if !os.IsNotExist(err) {
						t.Fatal("recovery changed file absence", file.Name, err)
					}
					continue
				}
				prefix := "old-"
				if committed {
					prefix = "new-"
				}
				if err != nil || string(data) != prefix+file.Name {
					t.Fatal("recovery chose wrong side of commit", file.Name, string(data), err)
				}
			}
			if err := RecoverFiles(context.Background(), reopened); err != nil {
				t.Fatal("recovery is not idempotent", err)
			}
		})
	}
}

func TestFileTransactionChild(t *testing.T) {
	root := os.Getenv("PROXY_FILE_TX_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	owner, err := panelstore.Try(filepath.Join(root, "panel.db"), filepath.Join(root, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	files, sources := simpleFiles(t, root)
	if err := installFiles(context.Background(), owner, files, sources, nil, func(point string) {
		if point == os.Getenv("PROXY_FILE_TX_EXIT") {
			if os.Getenv("PROXY_FILE_TX_AFTER_COMMIT_WRITE") == "1" {
				if err := os.WriteFile(files[0].Destination, []byte("legitimate-new-data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			os.Exit(77)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func stopFileTransaction(t *testing.T, point string, writeAfterCommit bool) (*panelstore.Ownership, []archive.RestoreFile) {
	t.Helper()
	owner, files, _ := fileFixture(t)
	root := filepath.Dir(owner.Database)
	owner.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFileTransactionChild$")
	command.Env = append(os.Environ(), "PROXY_FILE_TX_ROOT="+root, "PROXY_FILE_TX_EXIT="+point)
	if writeAfterCommit {
		command.Env = append(command.Env, "PROXY_FILE_TX_AFTER_COMMIT_WRITE=1")
	}
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 77 {
		t.Fatal("child did not reach checkpoint", err, string(output))
	}
	reopened, err := panelstore.Try(owner.Database, owner.IdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	return reopened, files
}

func TestFileRecoveryRefusesDamagedEvidenceAndForeignDestinationWithoutPartialRollback(t *testing.T) {
	for _, fault := range []string{"journal", "original", "replacement", "unexpected-file", "foreign-destination", "commit"} {
		t.Run(fault, func(t *testing.T) {
			point := "installed-1"
			if fault == "commit" {
				point = "committed"
			}
			owner, files := stopFileTransaction(t, point, false)
			root := filepath.Dir(owner.Database)
			journals, _ := filepath.Glob(filepath.Join(root, ".restore-txn-*"))
			if len(journals) != 1 {
				t.Fatal(journals)
			}
			var path string
			switch fault {
			case "journal":
				path = filepath.Join(root, journalName)
			case "original":
				path = filepath.Join(journals[0], "old", "0")
			case "replacement":
				path = filepath.Join(journals[0], "new", "0")
			case "unexpected-file":
				path = filepath.Join(journals[0], "unexpected")
			case "foreign-destination":
				path = files[3].Destination
			case "commit":
				path = filepath.Join(journals[0], "committed")
			}
			if err := os.WriteFile(path, []byte("foreign-or-corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := RecoverFiles(context.Background(), owner); err == nil {
				t.Fatal("damaged restore accepted", fault)
			}
			data, err := os.ReadFile(files[0].Destination)
			if err != nil || string(data) != "new-database" {
				t.Fatal("failed preflight partially rolled back other files", fault, string(data), err)
			}
			if _, err := os.Stat(filepath.Join(root, journalName)); err != nil {
				t.Fatal("failed recovery deleted journal", err)
			}
		})
	}
}

func TestCommittedRecoveryDoesNotDiscardLaterWrites(t *testing.T) {
	owner, files := stopFileTransaction(t, "committed", true)
	if err := RecoverFiles(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(files[0].Destination)
	if err != nil || string(data) != "legitimate-new-data" {
		t.Fatal("committed recovery replaced later data", err)
	}
}

func TestPreparedRecoverySidecarCleanupRefusesNonemptyWALBeforeRemovingIndex(t *testing.T) {
	owner, _, _ := fileFixture(t)
	wal := owner.Database + "-wal"
	index := owner.Database + "-shm"
	if err := os.WriteFile(wal, []byte("unconfirmed-wal-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, make([]byte, 32768), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPreparedSidecars(owner, filePlan{ReadOnlyPreflight: true}); !errors.Is(err, ErrFileRecovery) {
		t.Fatal("unconfirmed WAL discarded", err)
	}
	data, err := os.ReadFile(wal)
	if err != nil || string(data) != "unconfirmed-wal-data" {
		t.Fatal("failed sidecar preflight changed WAL", err)
	}
	if _, err := os.Stat(index); err != nil {
		t.Fatal("failed sidecar preflight removed index", err)
	}
}

func TestRealCandidateInstallsWithoutResettingUsageOrLeavingOldWAL(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	f.record(t, "install-later", 200, 400)
	if _, err := f.source.Database.Exec("UPDATE users SET display_name='later draft' WHERE id=?", f.user.ID); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallCandidate(ctx, f.owner, f.source, candidate, f.prepared, recoveryPassword); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(ctx, f.source.Config.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertInt(t, reopened, "SELECT used_traffic FROM users WHERE id=?", 600, f.user.ID)
	assertText(t, reopened, "SELECT display_name FROM users WHERE id=?", "Original name", f.user.ID)
	assertInt(t, reopened, "SELECT count(*) FROM sessions", 0)
	if err := RecoverFiles(ctx, f.owner); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateMustMatchCurrentEvidenceBeforeFilesCanChange(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.record(t, "newer-than-candidate", 300, 600)
	if err := InstallCandidate(ctx, f.owner, f.source, candidate, f.prepared, recoveryPassword); !errors.Is(err, ErrEvidence) {
		t.Fatal("stale candidate installed", err)
	}
	assertInt(t, f.source.Database, "SELECT used_traffic FROM users WHERE id=?", 900, f.user.ID)
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.owner.Database), journalName)); !os.IsNotExist(err) {
		t.Fatal("stale evidence created active file journal", err)
	}
}

func TestInstalledCandidateStartsActualPanelBinary(t *testing.T) {
	binary := os.Getenv("PANEL_BINARY")
	if binary == "" {
		t.Skip("set PANEL_BINARY for actual binary restart verification")
	}
	for _, exitPoint := range []string{"", "installed-0", "validated"} {
		t.Run(map[string]string{"": "committed", "installed-0": "startup-rollback", "validated": "prepared-startup-rollback"}[exitPoint], func(t *testing.T) {
			interrupted := exitPoint != ""
			f := recoveryFixture(t)
			ctx := context.Background()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			f.source.Config.HTTPAddress = listener.Addr().String()
			listener.Close()
			f.source.Config.GRPCAddress = "127.0.0.1:0"
			root := t.TempDir()
			created, err := f.source.Create(ctx, root, "actual_restart_01", "owner", recoveryPassword)
			if err != nil {
				t.Fatal(err)
			}
			f.prepared, err = archive.PrepareRestore(ctx, filepath.Join(t.TempDir(), "stages"), filepath.Join(root, created.Manifest.ID+".pcb"), recoveryPassword, archive.RestoreTarget{Panel: f.source.Config, ConfigurationPath: f.prepared.Plan.Target.ConfigurationPath})
			if err != nil {
				t.Fatal(err)
			}
			f.record(t, "after-restart-snapshot", 150, 300)
			if _, err := f.source.Database.Exec("UPDATE users SET display_name='latest-source-draft' WHERE id=?", f.user.ID); err != nil {
				t.Fatal(err)
			}
			currentConfiguration, err := yaml.Marshal(f.source.Config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.prepared.Plan.Target.ConfigurationPath, currentConfiguration, 0600); err != nil {
				t.Fatal(err)
			}
			candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
			if err != nil {
				t.Fatal(err)
			}
			if interrupted {
				f.source.Database.Close()
				f.owner.Close()
				installCtx, cancelInstall := context.WithTimeout(ctx, 20*time.Second)
				defer cancelInstall()
				installer := exec.CommandContext(installCtx, os.Args[0], "-test.run=^TestCandidateInstallChild$")
				installer.Env = append(os.Environ(), "PROXY_CANDIDATE_EXIT="+exitPoint, "PROXY_CANDIDATE_CONFIG="+f.prepared.Plan.Target.ConfigurationPath, "PROXY_CANDIDATE_DIR="+candidate.Directory, "PROXY_CANDIDATE_DIGEST="+candidate.ManifestSHA256, "PROXY_PREPARED_DIR="+f.prepared.Directory, "PROXY_PREPARED_DIGEST="+f.prepared.ManifestSHA256)
				output, err := installer.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 77 {
					t.Fatal("installer did not stop after replacing DB", err, string(output))
				}
			} else if err := InstallCandidate(ctx, f.owner, f.source, candidate, f.prepared, recoveryPassword); err != nil {
				t.Fatal(err)
			}
			if err := f.owner.Close(); err != nil {
				t.Fatal(err)
			}
			commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			command := exec.CommandContext(commandCtx, binary, "--config", f.prepared.Plan.Target.ConfigurationPath)
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
			deadline := time.Now().Add(10 * time.Second)
			for {
				response, err := client.Get("http://" + f.source.Config.HTTPAddress + "/healthz")
				if err == nil {
					response.Body.Close()
					if response.StatusCode == 200 {
						break
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("restored actual panel did not become healthy")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			command.Wait()
			finished = true
			reopened, err := db.Open(ctx, f.source.Config.Database)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertInt(t, reopened, "SELECT used_traffic FROM users WHERE id=?", 450, f.user.ID)
			if interrupted {
				assertText(t, reopened, "SELECT display_name FROM users WHERE id=?", "latest-source-draft", f.user.ID)
			} else {
				assertText(t, reopened, "SELECT display_name FROM users WHERE id=?", "Original name", f.user.ID)
				assertInt(t, reopened, "SELECT count(*) FROM sessions", 0)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(f.source.Config.Database), journalName)); !os.IsNotExist(err) {
				t.Fatal("actual startup left active restore journal", err)
			}
		})
	}
}

func TestCandidateInstallChild(t *testing.T) {
	path := os.Getenv("PROXY_CANDIDATE_CONFIG")
	if path == "" {
		t.Skip("subprocess only")
	}
	ctx := context.Background()
	configuration, err := config.LoadPanel(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := panelstore.Try(configuration.Database, configuration.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	database, err := db.Open(ctx, owner.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	key, err := vault.Open(filepath.Join(owner.IdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	source := archive.Source{Database: database, Config: configuration, ConfigurationVault: key}
	candidate := DatabaseCandidate{Directory: os.Getenv("PROXY_CANDIDATE_DIR"), ManifestSHA256: os.Getenv("PROXY_CANDIDATE_DIGEST")}
	prepared := archive.PreparedRestore{Directory: os.Getenv("PROXY_PREPARED_DIR"), ManifestSHA256: os.Getenv("PROXY_PREPARED_DIGEST")}
	if err := installCandidate(ctx, owner, source, candidate, prepared, recoveryPassword, func(point string) {
		if point == os.Getenv("PROXY_CANDIDATE_EXIT") {
			os.Exit(77)
		}
	}); err != nil {
		t.Fatal(err)
	}
}
