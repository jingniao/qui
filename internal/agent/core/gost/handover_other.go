//go:build !linux

package gost

import (
	"context"
	"os/exec"
	"proxycontrol/internal/agent/core"
)

func (a *Adapter) discardUnstartedRuntimeLocked() error { return nil }

func (a *Adapter) claimRuntimeLocked(_ context.Context, configuration core.RenderedConfig, epoch, _ string, _ *exec.Cmd) error {
	return a.runMarker().Claim(epoch, configuration.Revision)
}
func (a *Adapter) activateRuntimeLocked() error           { return nil }
func (a *Adapter) persistSampleRuntimeLocked() error      { return nil }
func (a *Adapter) recordRuntimeExitLocked() error         { return nil }
func (a *Adapter) removeRuntimeLocked(core.CoreRun) error { return nil }
func (a *Adapter) Adopt(context.Context, core.RuntimeMaterial, core.CoreRun) error {
	return core.ErrHandoverUnsupported
}
func (a *Adapter) PrepareHandover(context.Context) (core.HandoverCoreReceipt, error) {
	return core.HandoverCoreReceipt{}, core.ErrHandoverUnsupported
}
func (a *Adapter) DetachForHandover() error { return core.ErrHandoverUnsupported }
