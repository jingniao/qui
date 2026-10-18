//go:build !windows

package reconcile

import "os"

func syncReleaseHoldDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
