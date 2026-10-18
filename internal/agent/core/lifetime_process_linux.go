//go:build linux

package core

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// observeProcessIdentity reads the kernel start time (field 22, in clock ticks
// since boot) from /proc/<pid>/stat. It distinguishes a gone process (ENOENT)
// from an unobservable one so a genuine exit is provable and reuse of the PID by
// another process is detected by the identity mismatch.
func observeProcessIdentity(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) {
		return "", errProcessGone
	}
	if err != nil {
		return "", err
	}
	text := string(data)
	close := strings.LastIndexByte(text, ')')
	if close < 0 {
		return "", errProcessUnobservable
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) < 20 {
		return "", errProcessUnobservable
	}
	ticks := fields[19]
	if _, err := strconv.ParseUint(ticks, 10, 64); err != nil {
		return "", errProcessUnobservable
	}
	return "l" + ticks, nil
}
