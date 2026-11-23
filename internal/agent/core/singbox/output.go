package singbox

import "proxycontrol/internal/agent/core"

const outputLogLimit = 20 << 20

type runtimeTail interface {
	Checkpoint() (core.LogCheckpoint, error)
	Resume()
	Detach() error
	Err() error
	CompleteAttach() error
	CloseAfterExit() error
}

func (a *Adapter) closeOutputLocked() error {
	if a.outputTail == nil {
		return nil
	}
	err := a.outputTail.CloseAfterExit()
	a.outputTail = nil
	return err
}
