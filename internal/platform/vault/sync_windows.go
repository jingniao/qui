//go:build windows

package vault

// SyncDir is a no-op on Windows, where directory handles cannot be flushed.
// File data is synced before publication; whole-volume loss is unverified.
func SyncDir(string) error { return nil }
