//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package filelock

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrBusy
	}
	return err
}
func unlockFile(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }

func checkSingleLink(file *os.File) error {
	return checkLinkCount(file, 1)
}
func checkLinkCount(file *os.File, expected uint64) error {
	var info unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &info); err != nil {
		return err
	}
	if uint64(info.Nlink) != expected {
		return errors.New("persistent file cannot have hardlink aliases")
	}
	return nil
}
