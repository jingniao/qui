//go:build linux

package capability

import "syscall"

func diskBytes(directory string) (*uint64, *uint64) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(directory, &s); err != nil || s.Blocks == 0 || s.Bavail > s.Blocks || s.Bsize <= 0 {
		return nil, nil
	}
	total := s.Blocks * uint64(s.Bsize)
	used := (s.Blocks - s.Bavail) * uint64(s.Bsize)
	return &total, &used
}
