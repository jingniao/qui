//go:build linux

package core

import "syscall"

// CoreProcessAttributes is the process attribute every proxy core is started
// with. Setsid separates the core's session so an authorized update can detach
// it without parent-death signals. systemd KillMode=process and OpenRC
// rc_cgroup_cleanup=no leave core shutdown to ExecStopPost/stop_post barriers.
// Those barriers preserve cores only for a verified update handover; ordinary
// stops and crashes must prove core exit before a replacement can start.
func CoreProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
