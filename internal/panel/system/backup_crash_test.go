package system

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	"testing"
	"time"
)

func TestKilledBackupProcessRecoversJournalWithoutTouchingUnrelatedBackup(t *testing.T) {
	for _, checkpoint := range []string{"create-published", "delete-prepared", "delete-committed", "staging"} {
		t.Run(checkpoint, func(t *testing.T) {
			ctx := context.Background()
			s := backupFixture(t)
			item, err := s.Create(ctx, "owner", "ip", backupTestPassword)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(item)
			command := exec.Command(os.Args[0], "-test.run=^TestBackupCrashChild$")
			command.Env = append(os.Environ(), "PROXY_BACKUP_CRASH_ROOT="+filepath.Dir(s.Source.Config.Database), "PROXY_BACKUP_CRASH_POINT="+checkpoint, "PROXY_BACKUP_CRASH_ITEM="+string(body))
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if command.ProcessState == nil {
					command.Process.Kill()
					command.Wait()
				}
			}()
			ready := make(chan string, 1)
			go func() {
				scanner := bufio.NewScanner(output)
				for scanner.Scan() {
					if scanner.Text() == "checkpoint-ready" {
						ready <- scanner.Text()
						return
					}
				}
				ready <- ""
			}()
			select {
			case value := <-ready:
				if value == "" {
					command.Process.Kill()
					command.Wait()
					t.Fatal("child never reached crash checkpoint")
				}
			case <-time.After(12 * time.Second):
				command.Process.Kill()
				command.Wait()
				t.Fatal("child checkpoint timed out")
			}
			// Kill this exact helper while its real process lock and operation
			// journal are still active; no process-name or wildcard termination.
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			command.Wait()
			if err := s.Recover(ctx); err != nil {
				t.Fatal("killed process store did not recover", err)
			}
			if checkpoint == "delete-committed" {
				if _, err := os.Stat(filepath.Join(s.Directory, item.ID+".pcb")); !os.IsNotExist(err) {
					t.Fatal("committed delete recovered incorrectly", err)
				}
			} else {
				if _, err := s.Repository.Backup(ctx, item.ID); err != nil {
					t.Fatal("original committed backup lost", err)
				}
				if _, err := os.Stat(filepath.Join(s.Directory, item.ID+".pcb")); err != nil {
					t.Fatal("original committed backup file lost", err)
				}
			}
			entries, err := os.ReadDir(s.Directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != ".store.lock" && entry.Name() != item.ID+".pcb" {
					t.Fatal("crash recovery left owned staging/journal file", entry.Name())
				}
			}
		})
	}
}

func TestBackupCrashChild(t *testing.T) {
	root := os.Getenv("PROXY_BACKUP_CRASH_ROOT")
	if root == "" {
		t.Skip("only run by backup crash parent")
	}
	ctx := context.Background()
	c := config.DefaultPanel()
	c.Database = filepath.Join(root, "panel.db")
	c.NodeIdentityDirectory = filepath.Join(root, "identity")
	database, err := db.Open(ctx, c.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	key, err := vault.Open(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	s := &BackupService{Repository: &Repository{Database: database}, Source: archive.Source{Database: database, Config: c, ConfigurationVault: key}, Directory: filepath.Join(root, "backups")}
	done, err := s.beginStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	var item Backup
	if err := json.Unmarshal([]byte(os.Getenv("PROXY_BACKUP_CRASH_ITEM")), &item); err != nil {
		t.Fatal(err)
	}
	switch os.Getenv("PROXY_BACKUP_CRASH_POINT") {
	case "create-published":
		newID, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Repository.prepareBackupFile(ctx, Backup{ID: newID}, "create"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Source.Create(ctx, s.Directory, newID, "owner", backupTestPassword); err != nil {
			t.Fatal(err)
		}
	case "delete-prepared", "delete-committed":
		if err := s.Repository.prepareBackupFile(ctx, item, "delete"); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(s.Directory, item.ID+".pcb"), filepath.Join(s.Directory, ".trash-"+item.ID)); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("PROXY_BACKUP_CRASH_POINT") == "delete-committed" {
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec("DELETE FROM backups WHERE id=?", item.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec("UPDATE backup_file_ops SET phase='committed' WHERE backup_id=?", item.ID); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	case "staging":
		work, err := os.MkdirTemp(s.Directory, ".work-")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, ".panel-backup-stage"), []byte("proxy-control-backup-stage-v1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "private.key"), []byte("fixture-private-material"), 0600); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown checkpoint")
	}
	fmt.Println("checkpoint-ready")
	io.Copy(io.Discard, os.Stdin)
}
