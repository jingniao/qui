//go:build !linux

package gost

import (
	"errors"
	"os/exec"
)

func (a *Adapter) openErrorLogLocked(command *exec.Cmd) error { command.Stderr = &a.log; return nil }
func closeCommandErrorLog(*exec.Cmd) error                    { return nil }
func readErrorLogTail(string) (string, error) {
	return "", errors.New("file diagnostics unavailable on this platform")
}
