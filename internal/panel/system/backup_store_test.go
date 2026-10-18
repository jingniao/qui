package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/id"
	"testing"
)

func TestInterruptedDeleteRestoresPreparedFileAndFinishesCommittedRemoval(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "committed"}[committed], func(t *testing.T) {
			ctx := context.Background()
			s := backupFixture(t)
			item, err := s.Create(ctx, "owner", "ip", backupTestPassword)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.Directory, item.ID+".pcb")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Repository.prepareBackupFile(ctx, item, "delete"); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path, filepath.Join(s.Directory, ".trash-"+item.ID)); err != nil {
				t.Fatal(err)
			}
			if committed {
				tx, err := s.Repository.Database.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec("DELETE FROM backups WHERE id=?", item.ID); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				if _, err := tx.Exec("UPDATE backup_file_ops SET phase='committed' WHERE backup_id=?", item.ID); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			restarted := &BackupService{Repository: s.Repository, Source: s.Source, Directory: s.Directory}
			if err := restarted.Recover(ctx); err != nil {
				t.Fatal("interrupted deletion could not recover", err)
			}
			if committed {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("committed deletion was undone", err)
				}
			} else {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatal("prepared deletion lost original archive", err)
				}
				if _, err := s.Repository.Backup(ctx, item.ID); err != nil {
					t.Fatal("recovery altered original catalog", err)
				}
			}
			if _, err := os.Stat(filepath.Join(s.Directory, ".trash-"+item.ID)); !os.IsNotExist(err) {
				t.Fatal("recovery left deletion trash", err)
			}
			var count int
			s.Repository.Database.QueryRow("SELECT count(*) FROM backup_file_ops").Scan(&count)
			if count != 0 {
				t.Fatal("recovery preparation remained", count)
			}
			if err := restarted.Recover(ctx); err != nil {
				t.Fatal("recovery replay failed", err)
			}
		})
	}
}

func TestUncommittedCreateRecoveryRemovesOnlyJournalOwnedFile(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	manual, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
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
	unknown := filepath.Join(s.Directory, "unmanaged.pcb")
	if err := os.WriteFile(unknown, []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Directory, newID+".pcb")); !os.IsNotExist(err) {
		t.Fatal("uncommitted created file retained", err)
	}
	if _, err := os.Stat(filepath.Join(s.Directory, manual.ID+".pcb")); err != nil {
		t.Fatal("recovery removed committed manual backup", err)
	}
	if data, err := os.ReadFile(unknown); err != nil || string(data) != "preserved" {
		t.Fatal("recovery deleted unrelated file", err)
	}
}

func TestStoreOperationsAreExclusiveAcrossServiceHandlesAndInvalidRecoveryFailsClosed(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	done, err := s.beginStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other := &BackupService{Repository: s.Repository, Source: s.Source, Directory: s.Directory}
	if _, err := other.Create(ctx, "owner", "ip", backupTestPassword); !errors.Is(err, ErrBackupBusy) {
		done()
		t.Fatal("different service handle bypassed process lock", err)
	}
	done()
	idValue, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(Backup{ID: "different"})
	if _, err := s.Repository.Database.Exec("INSERT INTO backup_file_ops(backup_id,operation,phase,metadata_json,created_at) VALUES(?,'create','prepared',?,'fixture')", idValue, string(data)); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(ctx); !errors.Is(err, ErrBackupRecovery) {
		t.Fatal("inconsistent recovery metadata ignored", err)
	}
	if _, err := s.Create(ctx, "owner", "ip", backupTestPassword); !errors.Is(err, ErrBackupRecovery) {
		t.Fatal("new write bypassed inconsistent recovery", err)
	}
}

func TestDeletePreparationFromBeforeOriginMigrationRestoresLocalBackup(t *testing.T) {
	ctx := context.Background()
	s := backupFixture(t)
	item, err := s.Create(ctx, "owner", "ip", backupTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	legacy := item
	legacy.Origin = ""
	if err := s.Repository.prepareBackupFile(ctx, legacy, "delete"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(s.Directory, item.ID+".pcb"), filepath.Join(s.Directory, ".trash-"+item.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal("old deletion metadata cannot restore local backup", err)
	}
	current, err := s.Repository.Backup(ctx, item.ID)
	if err != nil || current.Origin != "local" {
		t.Fatal(current, err)
	}
	if _, err := os.Stat(filepath.Join(s.Directory, item.ID+".pcb")); err != nil {
		t.Fatal("old preparation did not restore file", err)
	}
}
