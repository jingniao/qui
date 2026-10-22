//go:build !linux

package core

import "syscall"

// CoreProcessAttributes returns no attributes off Linux: Pdeathsig is a Linux
// only field. On the other supported platform the packaged service manages the
// core's lifetime together with the Agent, and inventing a partial attribute
// set here would change how the core is started without any equivalent
// guarantee.
func CoreProcessAttributes() *syscall.SysProcAttr { return nil }
