package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"strings"
)

type CommandReceipt struct {
	RestartCurrent bool   `json:"restart_current,omitempty"`
	Revision       int64  `json:"revision"`
	Success        bool   `json:"success"`
	Code           string `json:"code"`
}

func (r *Reconciler) RollbackCommand(ctx context.Context, commandID string, revision int64) (bool, string, error) {
	return r.rollbackOperation(ctx, commandID, revision, false)
}
func (r *Reconciler) RestartCommand(ctx context.Context, commandID string, revision int64) (bool, string, error) {
	return r.rollbackOperation(ctx, commandID, revision, true)
}
func (r *Reconciler) rollbackOperation(ctx context.Context, commandID string, revision int64, restart bool) (bool, string, error) {
	if commandID == "" || len(commandID) > 128 || revision < 0 || (restart && revision == 0) || strings.ContainsAny(commandID, "/\\.") {
		return false, "invalid_command", errors.New("invalid rollback command ID")
	}
	for _, character := range commandID {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false, "invalid_command", errors.New("invalid rollback command ID")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.coreUpdateHold || r.controlOnly {
		return false, "release_recovery_required", core.ErrReleaseRecovery
	}
	directory := filepath.Join(r.Directory, "commands")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return false, "persistence_failed", err
	}
	path := filepath.Join(directory, commandID+".json")
	if data, err := os.ReadFile(path); err == nil {
		var receipt CommandReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return false, "persistence_failed", err
		}
		if receipt.Revision != revision || receipt.RestartCurrent != restart {
			return false, "invalid_command", errors.New("rollback command changed")
		}
		if receipt.Success {
			return true, receipt.Code, nil
		}
		return false, receipt.Code, errors.New("previous rollback failed")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, "persistence_failed", err
	}
	previous := r.manifest.Revision
	var err error
	if restart && r.manifest.Revision != revision {
		err = errors.New("restart revision is no longer current")
	} else if revision == 0 {
		err = r.resetLocked(ctx)
	} else {
		err = r.rollbackLocked(ctx, revision)
	}
	// An unconfirmed core stop must not be written as a terminal local failure.
	// The adapter retains the exact child and unresolved accounting, so a later
	// retry of the same command can still continue; a persisted rollback_failed
	// receipt would block that retry forever.
	if err != nil && r.stopUnconfirmed() {
		return false, "stop_unconfirmed", err
	}
	receipt := CommandReceipt{Revision: revision, Success: err == nil, Code: "rollback_success"}
	receipt.RestartCurrent = restart
	if err != nil {
		receipt.Code = "rollback_failed"
	}
	if restart {
		receipt.Code = "restart_success"
		if err != nil {
			receipt.Code = "restart_failed"
		}
	}
	data, encodeErr := json.Marshal(receipt)
	if encodeErr != nil {
		return false, "persistence_failed", encodeErr
	}
	if saveErr := writeFile(path, data); saveErr != nil {
		if receipt.Success {
			var undoErr error
			if previous == 0 {
				undoErr = r.resetLocked(ctx)
			} else {
				undoErr = r.rollbackLocked(ctx, previous)
			}
			if undoErr != nil {
				return false, "persistence_failed", errors.Join(saveErr, undoErr)
			}
		}
		return false, "persistence_failed", saveErr
	}
	return receipt.Success, receipt.Code, err
}

// stopUnconfirmed reports whether the core adapter's last forced stop could not
// confirm the exact child's exit. Its original process and accounting run are
// retained; no replacement was started.
func (r *Reconciler) stopUnconfirmed() bool {
	observer, ok := r.Adapter.(interface{ UnconfirmedStop() bool })
	return ok && observer.UnconfirmedStop()
}

func (r *Reconciler) resetLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	closer, ok := r.Adapter.(interface{ Close() error })
	if !ok {
		return errors.New("core cannot reset")
	}
	if err := closer.Close(); err != nil {
		return err
	}
	next := r.manifest
	next.Revision = 0
	next.Digest = ""
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(r.Directory, "current.json"), data); err != nil {
		if restoreErr := r.restoreRuntimeLocked(ctx, r.manifest.Revision); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	r.manifest = next
	r.renderedDigest = ""
	return nil
}
