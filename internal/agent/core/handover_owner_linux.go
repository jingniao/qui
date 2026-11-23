//go:build linux

package core

import (
	"errors"
	"os"
	"syscall"
)

// handoverFileOwner returns the owner new handover material must carry. The
// Agent runs as its service account, while the root stop barrier and CLI write
// into the same directories; a root-owned 0600 file there would be unreadable to
// the next Agent. Root therefore writes as the directory owner; any other
// caller keeps its own identity (-1, -1).
func handoverFileOwner(directory string) (int, int, error) {
	if os.Geteuid() != 0 {
		return -1, -1, nil
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.Join(ErrHandoverInvalid, errors.New("handover directory owner is unobservable"))
	}
	return int(stat.Uid), int(stat.Gid), nil
}
