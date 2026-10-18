package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/credential"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func restoreTargetFixture(t *testing.T) RestoreTarget {
	t.Helper()
	root := filepath.Join(t.TempDir(), "destination")
	panel := config.DefaultPanel()
	panel.Database = filepath.Join(root, "data", "restored.db")
	panel.NodeIdentityDirectory = filepath.Join(root, "identity")
	return RestoreTarget{Panel: panel, ConfigurationPath: filepath.Join(root, "etc", "panel.yaml")}
}

func TestRestoreStagePreservesMatchedDatabaseKeysAndRulesWithoutChangingTarget(t *testing.T) {
	ctx := context.Background()
	source, directory, secret := sourceFixture(t)
	created, err := source.Create(ctx, directory, "restore_stage_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(directory, created.Manifest.ID+".pcb")
	archiveBefore, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	target := restoreTargetFixture(t)
	// Existing unrelated target material must remain intact, including a key
	// that cannot decrypt the source database. Preparation must not replace it.
	if err := os.MkdirAll(target.Panel.NodeIdentityDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	unrelatedKey := bytes.Repeat([]byte{0x34}, 32)
	keyPath := filepath.Join(target.Panel.NodeIdentityDirectory, "configuration.key")
	if err := os.WriteFile(keyPath, unrelatedKey, 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRestore(ctx, directory, archivePath, testPassword, target)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source.ID != created.Manifest.ID || plan.ArchiveSHA256 != created.SHA256 || plan.ArchiveBytes != created.Bytes || !reflect.DeepEqual(plan.Target, target) {
		t.Fatal("stage binding lost", plan)
	}
	if _, err := os.Stat(target.Panel.Database); !os.IsNotExist(err) {
		t.Fatal("staging created a live database", err)
	}
	if _, err := os.Stat(target.ConfigurationPath); !os.IsNotExist(err) {
		t.Fatal("staging published target configuration", err)
	}
	afterKey, err := os.ReadFile(keyPath)
	if err != nil || !bytes.Equal(afterKey, unrelatedKey) {
		t.Fatal("staging changed target key", err)
	}
	database, err := readonlyDatabase(filepath.Join(prepared.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	key, err := os.ReadFile(filepath.Join(prepared.Directory, "identity", "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	configurationVault, err := vault.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err := database.QueryRow("SELECT encrypted_secret FROM credentials LIMIT 1").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	cleartext, err := configurationVault.Open(encrypted)
	var preserved credential.Secret
	if err != nil || json.Unmarshal(cleartext, &preserved) != nil || preserved.Value != secret {
		t.Fatal("staged DB is not paired with its original key", err)
	}
	clear(cleartext)
	clear(key)
	for _, item := range plan.Files {
		if item.Name == "identity/configuration.key" && item.Destination != keyPath {
			t.Fatal("wrong identity destination")
		}
		if item.Name == "rulesets/company/services.json" && item.Destination != filepath.Join(filepath.Dir(target.Panel.Database), "rulesets", "company", "services.json") {
			t.Fatal("wrong ruleset destination")
		}
	}
	if err := CleanupStages(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256); err != nil {
		t.Fatal("backup cleanup deleted persistent restore stage", err)
	}
	configuration, err := config.LoadPanel(filepath.Join(prepared.Directory, filepath.FromSlash(restoreConfigName)))
	if err != nil || !reflect.DeepEqual(configuration, target.Panel) {
		t.Fatal("relocated configuration lost target paths or endpoints", err)
	}
	archiveAfter, err := os.ReadFile(archivePath)
	if err != nil || !bytes.Equal(archiveBefore, archiveAfter) {
		t.Fatal("preparation modified portable backup", err)
	}
	if runtime.GOOS != "windows" {
		filepath.WalkDir(prepared.Directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				t.Fatal(walkErr)
			}
			info, err := entry.Info()
			if err != nil || info.Mode().Perm()&0077 != 0 {
				t.Fatal("staging material accessible to other users", path, err)
			}
			return nil
		})
	}
	database.Close()
	if err := DiscardPreparedRestore(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Directory); !os.IsNotExist(err) {
		t.Fatal("discard left plaintext staging", err)
	}
	assertNoStage(t, directory)
}

func TestRestoreStageRejectsTamperingSidecarsAndUnpinnedPlan(t *testing.T) {
	ctx := context.Background()
	source, directory, _ := sourceFixture(t)
	created, err := source.Create(ctx, directory, "restore_stage_002", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"key", "database", "target-configuration", "manifest", "missing", "wal", "extra-directory", "marker", "untrusted-digest", "symlink", "hardlink"} {
		t.Run(fault, func(t *testing.T) {
			prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, restoreTargetFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			path := ""
			switch fault {
			case "key":
				path = "identity/configuration.key"
			case "database":
				path = "database/panel.db"
			case "target-configuration":
				path = restoreConfigName
			case "manifest":
				path = restorePlanName
			case "marker":
				path = ".panel-restore-stage"
			case "missing":
				err = os.Remove(filepath.Join(prepared.Directory, "identity", "ca.key"))
			case "wal":
				err = os.WriteFile(filepath.Join(prepared.Directory, "database", "panel.db-wal"), []byte("stale"), 0600)
			case "extra-directory":
				err = os.Mkdir(filepath.Join(prepared.Directory, "old-jobs"), 0700)
			case "untrusted-digest":
				prepared.ManifestSHA256 = hex.EncodeToString(make([]byte, 32))
			case "symlink":
				original := filepath.Join(prepared.Directory, "identity", "ca.key")
				copyPath := filepath.Join(t.TempDir(), "outside")
				data, err := os.ReadFile(original)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(copyPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(copyPath, original); err != nil {
					t.Skip("symlink unavailable", err)
				}
			case "hardlink":
				if err := os.Link(filepath.Join(prepared.Directory, "identity", "configuration.key"), filepath.Join(t.TempDir(), "outside-key")); err != nil {
					t.Skip("hardlink unavailable", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if path != "" {
				fullPath := filepath.Join(prepared.Directory, filepath.FromSlash(path))
				data, err := os.ReadFile(fullPath)
				if err != nil || len(data) == 0 {
					t.Fatal(err)
				}
				data[len(data)/2] ^= 1
				if err := os.WriteFile(fullPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256); err == nil {
				t.Fatal("invalid restore stage accepted", fault)
			}
			if err := DiscardPreparedRestore(ctx, prepared); err == nil {
				t.Fatal("corrupt stage deleted without diagnosis")
			}
			if _, err := os.Stat(prepared.Directory); err != nil {
				t.Fatal("failed verification changed retained stage", err)
			}
		})
	}
}

func TestRestoreStagePreparationFailureCleansOnlyOwnedTemporaryMaterials(t *testing.T) {
	ctx := context.Background()
	source, directory, _ := sourceFixture(t)
	created, err := source.Create(ctx, directory, "restore_stage_003", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(directory, created.Manifest.ID+".pcb")
	for _, fault := range []string{"password", "cancel", "cancel-after-stage", "target-collision", "target-directory-collision", "invalid-endpoint", "production-missing-tls", "source-archive-target", "target-symlink", "ownership-lock-target", "backup-lock-target"} {
		t.Run(fault, func(t *testing.T) {
			target := restoreTargetFixture(t)
			password := testPassword
			operation := ctx
			switch fault {
			case "password":
				password = "wrong-backup-password"
			case "cancel":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				operation = canceled
			case "cancel-after-stage":
				operation = cancelWhenStagedDatabase{Context: ctx, directory: directory}
			case "target-collision":
				target.ConfigurationPath = filepath.Join(target.Panel.NodeIdentityDirectory, "configuration.key")
			case "target-directory-collision":
				target.Panel.Database = target.Panel.NodeIdentityDirectory
			case "invalid-endpoint":
				target.Panel.HTTPAddress = "0.0.0.0:8080"
			case "production-missing-tls":
				target.Panel.Development = false
			case "source-archive-target":
				target.Panel.Database = archivePath
			case "ownership-lock-target":
				target.Panel.Database = filepath.Join(filepath.Dir(target.Panel.Database), ".panel-owner.lock")
			case "backup-lock-target":
				target.ConfigurationPath = filepath.Join(filepath.Dir(target.ConfigurationPath), ".store.lock")
			case "target-symlink":
				if err := os.MkdirAll(target.Panel.NodeIdentityDirectory, 0700); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside-key")
				if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(target.Panel.NodeIdentityDirectory, "configuration.key")); err != nil {
					t.Skip("symlink unavailable", err)
				}
			}
			prepared, err := PrepareRestore(operation, directory, archivePath, password, target)
			if err == nil || prepared.Directory != "" {
				t.Fatal("failed staging returned usable candidate", fault, err)
			}
			if fault == "cancel-after-stage" && !errors.Is(err, context.Canceled) {
				t.Fatal("copy interruption did not preserve cancellation", err)
			}
			assertNoStage(t, directory)
			if _, err := Inspect(ctx, directory, archivePath, testPassword); err != nil {
				t.Fatal("failed preparation damaged source archive", err)
			}
		})
	}
}

type cancelWhenStagedDatabase struct {
	context.Context
	directory string
}

func (c cancelWhenStagedDatabase) Err() error {
	stages, _ := filepath.Glob(filepath.Join(c.directory, ".restore-*", "database", "panel.db"))
	if len(stages) > 0 {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestRestoreStageProductionTLSRelocationAndMissingMaterials(t *testing.T) {
	ctx := context.Background()
	source, directory, _ := sourceFixture(t)
	source.Config.Development = false
	source.Config.HTTPAddress, source.Config.GRPCAddress = "127.0.0.1:443", "127.0.0.1:9443"
	source.Config.TLS = config.TLS{Certificate: filepath.Join(source.Config.NodeIdentityDirectory, "server.crt"), Key: filepath.Join(source.Config.NodeIdentityDirectory, "server.key"), ClientCA: filepath.Join(source.Config.NodeIdentityDirectory, "ca.crt")}
	created, err := source.Create(ctx, directory, "restore_stage_004", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	target := restoreTargetFixture(t)
	target.Panel.Development = false
	target.Panel.TLS = config.TLS{Certificate: target.ConfigurationPath + ".crt", Key: target.ConfigurationPath + ".key", ClientCA: target.ConfigurationPath + ".ca"}
	prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range prepared.Plan.Files {
		if item.Name == "tls/server.key" && item.Destination != target.Panel.TLS.Key {
			t.Fatal("TLS material not mapped to target")
		}
	}
	if _, err := os.Stat(target.Panel.TLS.Key); !os.IsNotExist(err) {
		t.Fatal("preparation wrote live TLS key", err)
	}
	if err := DiscardPreparedRestore(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	// A development archive cannot satisfy a production target by borrowing
	// whatever happens to exist at the target server's certificate paths.
	source.Config.Development = true
	development, err := source.Create(ctx, directory, "restore_stage_005", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRestore(ctx, directory, filepath.Join(directory, development.Manifest.ID+".pcb"), testPassword, target); err == nil {
		t.Fatal("missing archived production TLS accepted")
	}
	assertNoStage(t, directory)
}

func TestRestoreStageStrictPlanEvenWithExplicitlyPinnedNewDigest(t *testing.T) {
	ctx := context.Background()
	source, directory, _ := sourceFixture(t)
	created, err := source.Create(ctx, directory, "restore_stage_006", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, restoreTargetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(prepared.Directory, restorePlanName)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"unknown", "duplicate-file", "destination", "extra-json"} {
		data := append([]byte(nil), original...)
		if fault == "unknown" {
			data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"execute":true`), 1)
		}
		if fault == "extra-json" {
			data = append(data, []byte(`{}`)...)
		}
		if fault == "duplicate-file" || fault == "destination" {
			var plan RestorePlan
			if err := json.Unmarshal(data, &plan); err != nil {
				t.Fatal(err)
			}
			if fault == "duplicate-file" {
				plan.Files = append(plan.Files, plan.Files[0])
			} else {
				plan.Files[0].Destination = filepath.Join(t.TempDir(), "unexpected")
			}
			data, err = json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if _, err := VerifyPreparedRestore(ctx, prepared.Directory, hex.EncodeToString(digest[:])); err == nil {
			t.Fatal("invalid pinned restore plan accepted", fault)
		}
	}
}

func TestBackupAndRestoreRefuseIncompleteOrForeignDevelopmentServerIdentity(t *testing.T) {
	for _, fault := range []string{"key", "missing-key", "foreign-pair"} {
		t.Run(fault, func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			keyPath := filepath.Join(source.Config.NodeIdentityDirectory, "server.key")
			switch fault {
			case "key":
				if err := os.WriteFile(keyPath, []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-key":
				if err := os.Remove(keyPath); err != nil {
					t.Fatal(err)
				}
			case "foreign-pair":
				other, _, _ := sourceFixture(t)
				for _, name := range []string{"server.crt", "server.key"} {
					data, err := os.ReadFile(filepath.Join(other.Config.NodeIdentityDirectory, name))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(source.Config.NodeIdentityDirectory, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := source.Create(context.Background(), directory, "restore_stage_bad", "owner", testPassword); err == nil {
				t.Fatal("unusable development identity archived")
			}
			assertNoStage(t, directory)
			if fault == "missing-key" {
				if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
					t.Fatal("backup generated replacement identity", err)
				}
			}
		})
	}
}

func TestCompletedRestoreStageReopensInAnotherProcess(t *testing.T) {
	ctx := context.Background()
	source, directory, _ := sourceFixture(t)
	created, err := source.Create(ctx, directory, "restore_process_01", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, restoreTargetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, os.Args[0], "-test.run=^TestRestoreStageChild$")
	command.Env = append(os.Environ(), "PROXY_RESTORE_STAGE_PATH="+prepared.Directory, "PROXY_RESTORE_STAGE_DIGEST="+prepared.ManifestSHA256, "PROXY_RESTORE_STAGE_ID="+created.Manifest.ID)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal("completed stage cannot reopen from another process", err, string(output))
	}
	if err := DiscardPreparedRestore(ctx, prepared); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreStageChild(t *testing.T) {
	path := os.Getenv("PROXY_RESTORE_STAGE_PATH")
	if path == "" {
		t.Skip("subprocess only")
	}
	plan, err := VerifyPreparedRestore(context.Background(), path, os.Getenv("PROXY_RESTORE_STAGE_DIGEST"))
	if err != nil || plan.Source.ID != os.Getenv("PROXY_RESTORE_STAGE_ID") {
		t.Fatal("stage binding or contents invalid in second process", err)
	}
}
