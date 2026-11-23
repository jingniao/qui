//go:build linux

package gost

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func (a *Adapter) openErrorLogLocked(command *exec.Cmd) error {
	path, err := filepath.Abs(filepath.Join(a.Directory, "error.log"))
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		return errors.Join(errors.New("unsafe gost error log"), file.Close())
	}
	// The previous run's exit was proved before entering the new-start path.
	if err := file.Truncate(0); err != nil {
		return errors.Join(err, file.Close())
	}
	a.log.path = path
	command.Stderr = file
	return nil
}
func closeCommandErrorLog(command *exec.Cmd) error {
	if file, ok := command.Stderr.(*os.File); ok {
		return file.Close()
	}
	return nil
}
func readErrorLogTail(path string) (string, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	info, err := file.Stat()
	if err != nil {
		return "", errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		return "", errors.Join(errors.New("gost error log is not a regular file"), file.Close())
	}
	offset := info.Size() - childLogLimit
	if offset < 0 {
		offset = 0
	}
	data, err := io.ReadAll(io.NewSectionReader(file, offset, info.Size()-offset))
	return strings.TrimSpace(string(data)), errors.Join(err, file.Close())
}
