//go:build linux

package core

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func HandoverBoottimeNS() (int64, error) {
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &now); err != nil {
		return 0, errors.Join(ErrHandoverUnsupported, err)
	}
	return now.Nano(), nil
}

func checkHandoverUpdater(identity ProcessIdentity, environment OSEnvironment) error {
	token, state, err := observeLinuxProcess(identity.PID)
	if errors.Is(err, errProcessGone) {
		return errors.Join(ErrHandoverExpired, err)
	}
	if err != nil {
		return errors.Join(ErrHandoverConflict, err)
	}
	// PID plus kernel start time identifies the live updater. Its PID namespace
	// was proven by root when the request was armed; the unprivileged Agent may
	// not read a root process's /proc/<pid>/ns link, so it is not re-read here.
	if token != identity.Token || state == "Z" || state == "X" {
		return ErrHandoverExpired
	}
	return nil
}

func requestGroupID() (int, error) {
	group, err := user.LookupGroup("proxy-agent")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(group.Gid)
}

func requestOwner(path string, directory bool, gid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	mode := os.FileMode(0640)
	if directory {
		mode = 0750
	}
	if !ok || stat.Uid != 0 || stat.Gid != uint32(gid) || info.Mode().Perm() != mode || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || directory != info.IsDir() || !directory && !info.Mode().IsRegular() {
		return ErrHandoverPermission
	}
	return nil
}

func checkRequestPermissions() error {
	directory := filepath.Dir(HandoverRequestPath)
	if err := checkHandoverDirectory(directory); err != nil {
		return err
	}
	gid, err := requestGroupID()
	if err != nil {
		return err
	}
	if err := requestOwner(directory, true, gid); err != nil {
		return err
	}
	return requestOwner(HandoverRequestPath, false, gid)
}

func prepareRequestDirectory() (int, error) {
	if os.Geteuid() != 0 {
		return 0, ErrHandoverPermission
	}
	gid, err := requestGroupID()
	if err != nil {
		return 0, err
	}
	directory := filepath.Dir(HandoverRequestPath)
	if err := checkHandoverDirectory(filepath.Dir(directory)); err != nil {
		return 0, err
	}
	err = os.Mkdir(directory, 0750)
	if err == nil {
		if err := os.Chown(directory, 0, gid); err != nil {
			return 0, err
		}
		if err := os.Chmod(directory, 0750); err != nil {
			return 0, err
		}
		if err := syncAccountingDirectory(filepath.Dir(directory)); err != nil {
			return 0, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return 0, err
	}
	if err := requestOwner(directory, true, gid); err != nil {
		return 0, err
	}
	return gid, nil
}
