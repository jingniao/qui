package filelock

import (
	"errors"
	"os"
)

// TryExisting never creates a directory or lock file, including disappearance
// between observation and open. It keeps the normal single-link lock checks.
func TryExisting(path string) (*Lock, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("original lock must be a regular file")
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	after, afterErr := os.Lstat(path)
	if err != nil || afterErr != nil || !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		file.Close()
		return nil, errors.New("original lock changed while opening")
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
