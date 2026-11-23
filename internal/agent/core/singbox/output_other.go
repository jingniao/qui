//go:build !linux

package singbox

import (
	"io"
	"os/exec"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
)

func (a *Adapter) openOutputLocked(command *exec.Cmd, config core.RenderedConfig) error {
	command.Stdout = io.Discard
	a.sourceWriter = nil
	if config.AccessContext != nil && a.hooks.AccessSink != nil {
		a.sourceWriter = collector.NewSingboxWriter(config.AccessContext, a.hooks.AccessSink, a.hooks.AccessRejected)
		command.Stdout = a.sourceWriter
	}
	return nil
}
func closeCommandOutput(*exec.Cmd) error { return nil }
