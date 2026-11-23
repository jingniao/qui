//go:build windows

package statereset

// Production reset is Linux-only. Windows runs pure journal boundary tests.
func syncDirectory(string) error { return nil }
