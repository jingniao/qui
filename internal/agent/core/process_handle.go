package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
)

// ExitObservation is immutable after Exited becomes true. Waited distinguishes
// a real child's Wait result from an external exit whose exit code is unknown.
// Err also exposes observer failures; an observer failure never proves exit.
type ExitObservation struct {
	Exited   bool
	Waited   bool
	ExitCode *int
	Err      error
}

type ProcessHandle interface {
	Identity() ProcessIdentity
	Environment() OSEnvironment
	ExitObservation() ExitObservation
	Done() <-chan struct{}
	Detached() bool
	Stop(context.Context) error
	Detach() error
}

var ErrProcessDetached = errors.New("core process handle is detached")

type processHandle struct {
	mu          sync.Mutex
	identity    ProcessIdentity
	environment OSEnvironment
	done        chan struct{}
	observed    chan struct{}
	exit        ExitObservation
	detached    bool
	detachErr   error
	stop        func() error
	release     func() error
}

func (h *processHandle) Identity() ProcessIdentity  { return h.identity }
func (h *processHandle) Environment() OSEnvironment { return h.environment }
func (h *processHandle) Done() <-chan struct{}      { return h.done }
func (h *processHandle) Detached() bool             { h.mu.Lock(); defer h.mu.Unlock(); return h.detached }
func (h *processHandle) ExitObservation() ExitObservation {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := h.exit
	if result.ExitCode != nil {
		code := *result.ExitCode
		result.ExitCode = &code
	}
	return result
}

func (h *processHandle) observe(exit ExitObservation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.exit.Exited {
		return
	}
	h.exit = exit
	if exit.Exited {
		close(h.done)
	}
	close(h.observed)
}

// Stop kills the exact held process and waits for a broadcast exit fact. A
// caller deadline never consumes the eventual observation or a second Stop.
func (h *processHandle) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	if h.detached {
		h.mu.Unlock()
		return ErrProcessDetached
	}
	if h.exit.Exited {
		h.mu.Unlock()
		return nil
	}
	if h.exit.Err != nil {
		err := h.exit.Err
		h.mu.Unlock()
		return errors.Join(ErrCoreStopUnconfirmed, err)
	}
	err := h.stop()
	h.mu.Unlock()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-h.observed:
		exit := h.ExitObservation()
		if exit.Exited {
			return nil
		}
		return errors.Join(ErrCoreStopUnconfirmed, exit.Err)
	case <-ctx.Done():
		return errors.Join(ErrCoreStopUnconfirmed, ctx.Err())
	}
}

// Detach releases this Agent's control only. The real child waiter must still
// reap the child once; neither detach nor closing an observer proves exit.
func (h *processHandle) Detach() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.detached {
		return h.detachErr
	}
	// Linux close errors do not permit retrying a numeric fd: it may already
	// have been released and reused. Relinquish control even on release failure.
	h.detached = true
	if h.release != nil {
		h.detachErr = h.release()
	}
	return h.detachErr
}

// NewChildProcessHandle takes sole ownership of cmd.Wait after a successful
// return. Call only after Start and Bind; no other caller may call Wait.
func NewChildProcessHandle(cmd *exec.Cmd, run CoreRun) (ProcessHandle, error) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid != run.Process.PID {
		return nil, errors.Join(ErrHandoverInvalid, errors.New("child process does not match run"))
	}
	if err := validateCoreRun(&run); err != nil {
		return nil, err
	}
	token, err := ObserveProcessIdentity(cmd.Process.Pid)
	if err != nil {
		return nil, err
	}
	if token != run.Process.Token || !completeEnvironment(run.Environment) || run.Environment != ObserveOSEnvironment() {
		return nil, ErrHandoverConflict
	}
	h := &processHandle{identity: run.Process, environment: run.Environment, done: make(chan struct{}), observed: make(chan struct{}), stop: cmd.Process.Kill}
	go func() {
		err := cmd.Wait()
		if cmd.ProcessState == nil {
			h.observe(ExitObservation{Waited: true, Err: err})
			return
		}
		code := cmd.ProcessState.ExitCode()
		h.observe(ExitObservation{Exited: true, Waited: true, ExitCode: &code, Err: err})
	}()
	return h, nil
}
