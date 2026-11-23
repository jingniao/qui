//go:build !linux

package core

// Windows keeps child/Wait ownership; external adoption is Linux-only.
func OpenProcessHandle(CoreRun) (ProcessHandle, error) { return nil, ErrHandoverUnsupported }
