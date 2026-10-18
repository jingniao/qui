// Package panelstore owns the panel's persistent directories for its lifetime.
// Offline recovery must acquire the same locks before replacing any materials.
package panelstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/filelock"
	"runtime"
	"strings"
)

const lockName = ".panel-owner.lock"

type Ownership struct {
	Database          string
	IdentityDirectory string
	locks             []*filelock.Lock
	lockedDatabase    string
	lockedIdentity    string
}

// Try locks both the database parent (including rulesets/backups) and identity
// directory. Paths through directory aliases share the same permanent lock.
// Separate panels need separate directories, even with different DB filenames.
func Try(database, identityDirectory string) (owned *Ownership, resultErr error) {
	if database == "" || identityDirectory == "" {
		return nil, errors.New("panel database and identity directory are required")
	}
	dbAbsolute, err := filepath.Abs(database)
	if err != nil {
		return nil, err
	}
	if filepath.Base(dbAbsolute) == lockName || runtime.GOOS == "windows" && strings.EqualFold(filepath.Base(dbAbsolute), lockName) {
		return nil, errors.New("panel database cannot use the ownership lock filename")
	}
	dbDirectory, dbInfo, err := canonicalDirectory(filepath.Dir(dbAbsolute))
	if err != nil {
		return nil, err
	}
	identity, identityInfo, err := canonicalDirectory(identityDirectory)
	if err != nil {
		return nil, err
	}
	owner := &Ownership{Database: filepath.Join(dbDirectory, filepath.Base(dbAbsolute)), IdentityDirectory: identity}
	owner.lockedDatabase, owner.lockedIdentity = owner.Database, owner.IdentityDirectory
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owner.Close())
		}
	}()
	directories := []string{dbDirectory}
	if !os.SameFile(dbInfo, identityInfo) {
		directories = append(directories, identity)
	}
	for _, directory := range directories {
		lock, err := filelock.Try(filepath.Join(directory, lockName))
		if err != nil {
			return nil, fmt.Errorf("acquire panel directory ownership: %w", err)
		}
		owner.locks = append(owner.locks, lock)
	}
	// SQLite WAL filenames are path-based. Hardlinked DB files can otherwise
	// acquire different directory locks and inconsistent independent WALs.
	for _, path := range []string{owner.Database, owner.Database + "-wal", owner.Database + "-shm"} {
		if err := uniqueRegularFile(path); err != nil {
			return nil, err
		}
	}
	return owner, nil
}

func canonicalDirectory(path string) (string, os.FileInfo, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return "", nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() {
		return "", nil, errors.New("panel store must be a directory")
	}
	return canonical, info, nil
}

func uniqueRegularFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("panel database and WAL files must be regular files without links")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("panel database file changed while acquiring ownership")
	}
	return filelock.CheckSingleLink(file)
}

func (o *Ownership) Close() error {
	if o == nil {
		return nil
	}
	var err error
	for i := len(o.locks) - 1; i >= 0; i-- {
		err = errors.Join(err, o.locks[i].Close())
	}
	o.locks = nil
	return err
}

func (o *Ownership) AssertHeld(database, identity string) error {
	if o == nil || len(o.locks) == 0 || database != o.lockedDatabase || identity != o.lockedIdentity || o.Database != o.lockedDatabase || o.IdentityDirectory != o.lockedIdentity {
		return filelock.ErrBusy
	}
	for _, lock := range o.locks {
		if err := lock.AssertHeld(); err != nil {
			return err
		}
	}
	return nil
}
