//go:build !windows

package restoretasks

import "os"

func syncProgressDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
