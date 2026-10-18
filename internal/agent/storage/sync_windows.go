//go:build windows

package storage

import "os"

// File data is flushed before publication; whole-volume loss is unverified.
func syncDirectory(*os.Root) error { return nil }
