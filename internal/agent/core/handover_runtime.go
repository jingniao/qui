package core

import (
	"context"
	"errors"
	"os"
)

// HandoverRuntime is serialized by each adapter's lifecycle mutex. The caller
// must quiesce publishers, samplers and gates before preparing, and authorize
// the transaction before detaching. Close always retains stop semantics.
type HandoverRuntime interface {
	PrepareHandover(context.Context) (HandoverCoreReceipt, error)
	DetachForHandover() error
	Adopt(context.Context, RuntimeMaterial, CoreRun) error
}

// DiscardUnstartedRuntimeMaterial is only for a launch failure before Start
// succeeds. A crash's uncertain launch window must never use this cleanup.
func DiscardUnstartedRuntimeMaterial(directory string, expected CoreRun) error {
	if expected.Process.PID != 0 {
		return ErrHandoverConflict
	}
	run, _, err := LoadCoreRun(directory)
	if err != nil {
		return err
	}
	if run != nil {
		return ErrHandoverConflict
	}
	key, err := runtimeKey(directory)
	if err != nil {
		return err
	}
	var material RuntimeMaterial
	if err := loadEncryptedMaterial(CoreRuntimePath(directory), key, &material); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := material.Validate(); err != nil {
		return err
	}
	if material.State != RuntimePrepared || material.Run != expected {
		return ErrHandoverConflict
	}
	return removeHandoverMaterial(CoreRuntimePath(directory))
}
