//go:build windows

package identity

import (
	"errors"

	"golang.org/x/sys/windows"
)

func sharingConflict(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
