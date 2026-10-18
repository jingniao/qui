//go:build !windows

package vault

import "os"

// SyncDir flushes a directory entry (a created or renamed file) to stable
// storage so a crash cannot lose the directory update.
func SyncDir(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
