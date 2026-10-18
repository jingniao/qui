//go:build !linux && !windows

package core

// The Agent storage contract only admits linux and windows. Other platforms
// cannot authenticate a recorded exit, so the answer is deliberately Unknown:
// the marker stays and the data plane stays closed rather than guessing.
func observeProcessIdentity(int) (string, error) { return "", errProcessUnobservable }
