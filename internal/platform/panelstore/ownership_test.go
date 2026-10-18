package panelstore

import (
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/filelock"
	"runtime"
	"testing"
)

func TestDatabaseAndIdentityOwnershipAndPartialRelease(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "data", "panel.db")
	identity := filepath.Join(root, "identity")
	first, err := Try(database, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	otherDB := filepath.Join(root, "other", "panel.db")
	for _, pair := range [][2]string{{database, filepath.Join(root, "different-identity")}, {otherDB, identity}, {filepath.Join(root, "data", "other.db"), filepath.Join(root, "third-identity")}} {
		if owner, err := Try(pair[0], pair[1]); owner != nil || !errors.Is(err, filelock.ErrBusy) {
			t.Fatal("shared database directory or identity acquired", owner, err)
		}
	}
	// Failure on the second identity lock must release the first data lock.
	independent, err := Try(otherDB, filepath.Join(root, "other-identity"))
	if err != nil {
		t.Fatal("partially acquired lock leaked", err)
	}
	independent.Close()
	first.Close()
	second, err := Try(database, identity)
	if err != nil {
		t.Fatal("closed owner did not release both locks", err)
	}
	second.Close()
	if _, err := os.Stat(filepath.Join(filepath.Dir(database), lockName)); err != nil {
		t.Fatal("permanent lock was removed", err)
	}
}

func TestSameDirectoryAndDirectoryAliases(t *testing.T) {
	root := t.TempDir()
	owner, err := Try(filepath.Join(root, "panel.db"), filepath.Join(root, "."))
	if err != nil {
		t.Fatal("same physical directory was locked twice", err)
	}
	defer owner.Close()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip("directory symlink unavailable", err)
	}
	if second, err := Try(filepath.Join(alias, "panel.db"), alias); second != nil || !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("directory alias bypassed lifetime lock", err)
	}
}

func TestOwnershipLockCannotBeTheSQLiteDatabase(t *testing.T) {
	root := t.TempDir()
	if owner, err := Try(filepath.Join(root, lockName), root); owner != nil || err == nil {
		t.Fatal("SQLite database can overwrite ownership lock", err)
	}
	if runtime.GOOS == "windows" {
		if owner, err := Try(filepath.Join(root, ".PANEL-OWNER.LOCK"), root); owner != nil || err == nil {
			t.Fatal("Windows filename alias can overwrite lock", err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid database path created lock or runtime state", err)
	}
}

func TestAssertHeldRejectsMutatedTargetAndClosedOwner(t *testing.T) {
	root := t.TempDir()
	owner, err := Try(filepath.Join(root, "panel.db"), filepath.Join(root, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := owner.AssertHeld(owner.Database, owner.IdentityDirectory); err != nil {
		t.Fatal(err)
	}
	if err := owner.AssertHeld(filepath.Join(root, "other.db"), owner.IdentityDirectory); err == nil {
		t.Fatal("ownership validated another target")
	}
	original := owner.Database
	owner.Database = filepath.Join(root, "forged.db")
	if err := owner.AssertHeld(original, owner.IdentityDirectory); err == nil {
		t.Fatal("modified public handle bypassed locked path")
	}
	owner.Database = original
	owner.Close()
	if err := owner.AssertHeld(original, owner.IdentityDirectory); err == nil {
		t.Fatal("closed ownership handle validated")
	}
}

func TestLinkedAndNonregularDatabaseFilesRefused(t *testing.T) {
	for _, fault := range []string{"hardlink", "wal-hardlink", "shm-hardlink", "symlink", "directory"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			database := filepath.Join(root, "panel.db")
			path := database
			if fault == "wal-hardlink" {
				path += "-wal"
			}
			if fault == "shm-hardlink" {
				path += "-shm"
			}
			source := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(source, []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch fault {
			case "symlink":
				err = os.Symlink(source, path)
			case "directory":
				err = os.Mkdir(path, 0700)
			default:
				err = os.Link(source, path)
			}
			if err != nil {
				t.Skip("link creation unavailable", err)
			}
			if owner, err := Try(database, filepath.Join(root, "identity")); err == nil || owner != nil {
				t.Fatal("unsafe database path acquired", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			owner, err := Try(database, filepath.Join(root, "identity"))
			if err != nil {
				t.Fatal("file check failure leaked ownership", err)
			}
			owner.Close()
		})
	}
}
