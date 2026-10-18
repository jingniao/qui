//go:build windows

package core

import (
	"errors"
	"strconv"

	"golang.org/x/sys/windows"
)

// processStillActive is the exit-code value GetExitCodeProcess reports while a
// process is still running (STILL_ACTIVE, 259).
const processStillActive = 259

// observeProcessIdentity opens the pid with query-only rights, checks the process
// exit state and reads its creation time. A handle can outlive its process (a
// caller may still hold an open handle to a process that has exited), so the exit
// code is queried first: an exited-but-retained handle is a proven exit rather
// than a false "alive". A missing pid (ERROR_INVALID_PARAMETER) also proves the
// original child exited; a different creation time proves the PID was reused.
func observeProcessIdentity(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return "", errProcessGone
		}
		return "", err
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return "", err
	}
	if code != processStillActive {
		return "", errProcessGone
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return "", err
	}
	return "w" + strconv.FormatInt(creation.Nanoseconds(), 10), nil
}
