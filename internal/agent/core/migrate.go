package core

import (
	"errors"
	"os"
	"path/filepath"
)

// MigratePrimaryRuntime moves legacy flat files under the primary core while
// the Agent owns the state directory. Each rename is restartable: a crash leaves
// the remaining flat files to be moved by the next start. Conflicts are refused.
func MigratePrimaryRuntime(directory, primary string) error {
	if primary != "xray" && primary != "sing-box" {
		return errors.New("invalid primary core")
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	target := filepath.Join(directory, primary)
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid primary runtime directory")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("legacy runtime contains a symlink")
		}
		to := filepath.Join(target, entry.Name())
		if _, err := os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
			return errors.New("legacy runtime migration conflicts with per-core state")
		}
		if err := os.Rename(filepath.Join(directory, entry.Name()), to); err != nil {
			return err
		}
		if err := syncAccountingDirectory(target); err != nil {
			return err
		}
		if err := syncAccountingDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}
