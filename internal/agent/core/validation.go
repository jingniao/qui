package core

import (
	"context"
	"errors"
	"os/exec"
)

// ValidationRejectedError means the native validator ran and rejected the config.
type ValidationRejectedError struct{ Err error }

func (e *ValidationRejectedError) Error() string { return e.Err.Error() }
func (e *ValidationRejectedError) Unwrap() error { return e.Err }

// NativeValidationError keeps execution failures distinguishable from a rejection.
// The caller supplies the command context and may wrap the original error.
func NativeValidationError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(err, ctxErr)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.Exited() && exit.ExitCode() != 0 {
		return &ValidationRejectedError{Err: err}
	}
	return err
}
