//go:build !linux && !windows

package core

// observeOSEnvironment on an unsupported platform reports no environment fact.
// An empty environment keeps identity classification at the conservative PID
// probe, never a fabricated exit.
func observeOSEnvironment() OSEnvironment { return OSEnvironment{} }

// completeEnvironment is false on a platform that supplies no host/boot/namespace
// fact, so a recorded child is never resolved from an absent environment.
func completeEnvironment(OSEnvironment) bool { return false }
