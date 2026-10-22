//go:build linux

package core

import "syscall"

// CoreProcessAttributes is the process attribute every proxy core is started
// with. Pdeathsig makes the kernel send SIGKILL to the core when the OS thread
// that started it exits, so the core cannot outlive the Agent it belongs to.
//
// The Agent cannot rely on its supervisor for that. systemd's KillMode=mixed
// kills the whole cgroup, but OpenRC's supervise-daemon only signals the Agent
// PID it recorded: a crashed or SIGKILLed Agent worker would leave the core
// running and holding its ports, and the respawned Agent would then refuse to
// start a replacement because the durable lifetime marker still names a live
// child (ErrCoreStopUnconfirmed).
//
// Pdeathsig is bound to the starting thread rather than to the process. Go
// destroys an OS thread only when the goroutine locked to it exits, and the
// goroutine that starts a core is not locked, so that thread outlives the start
// call and the core dies with the Agent process as intended.
func CoreProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
