//go:build windows

package reconcile

// File contents are flushed before rename. Portable directory fsync is not
// available; Windows abrupt-volume-loss acceptance is still required.
func syncReleaseHoldDirectory(string) error { return nil }
