//go:build windows

package agenthost

// File data is flushed; whole-volume power-loss durability remains unverified.
func syncDirectory(string) error { return nil }
