//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package filelock

import (
	"errors"
	"os"
)

func lockFile(*os.File) error {
	return errors.New("process store locks unsupported on this operating system")
}
func unlockFile(*os.File) error { return nil }
func checkSingleLink(*os.File) error {
	return errors.New("persistent file link checks unsupported on this operating system")
}
func checkLinkCount(*os.File, uint64) error {
	return errors.New("persistent file link checks unsupported on this operating system")
}
