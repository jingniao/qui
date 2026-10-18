//go:build windows

package identity

// Keep original file synchronization; portable directory Sync is unavailable.
func syncAckDirectory(string) error { return nil }
