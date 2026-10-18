// Package filelock provides fail-fast process locks for local persistent stores.
package filelock

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrBusy = errors.New("persistent store is locked by another process")

type Lock struct{ file *os.File }

func Try(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("store lock must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, statErr := os.Lstat(path)
	opened, openedErr := file.Stat()
	if statErr != nil || openedErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, opened) {
		file.Close()
		return nil, errors.New("store lock changed while opening")
	}
	if err := CheckSingleLink(file); err != nil {
		file.Close()
		return nil, err
	}
	if err := lockFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return &Lock{file: file}, nil
}

// CheckSingleLink rejects regular files with external hardlink aliases. The
// caller must keep the handle open and separately validate its file type/path.
func CheckSingleLink(file *os.File) error { return checkSingleLink(file) }

// CheckLinkCount is for a fixed authenticated no-replace publication pair.
func CheckLinkCount(file *os.File, expected uint64) error { return checkLinkCount(file, expected) }

// AssertHeld detects a closed handle or replacement of the permanent lock
// path. Callers must still serialize Close with their protected operations.
func (l *Lock) AssertHeld() error {
	if l == nil || l.file == nil {
		return ErrBusy
	}
	opened, err := l.file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(l.file.Name())
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return errors.New("persistent store ownership path changed")
	}
	return CheckSingleLink(l.file)
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(unlockFile(file), file.Close())
}
