//go:build windows

package filelock

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func lockFile(file *os.File) error {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrBusy
	}
	return err
}
func unlockFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}

func checkSingleLink(file *os.File) error {
	return checkLinkCount(file, 1)
}
func checkLinkCount(file *os.File, expected uint64) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return err
	}
	if uint64(info.NumberOfLinks) != expected {
		return errors.New("persistent file cannot have hardlink aliases")
	}
	return nil
}
