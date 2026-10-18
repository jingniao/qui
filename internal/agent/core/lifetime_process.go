package core

import "errors"

var errProcessGone = errors.New("recorded core process is no longer present")
var errProcessUnobservable = errors.New("core process identity cannot be observed on this platform")

// ProcessState is the three-way answer to "is the recorded child still running".
// Unknown is a real answer and must be treated as "not proven exited".
type ProcessState int

const (
	ProcessUnknown ProcessState = iota
	ProcessAlive
	ProcessExited
)

// ObserveProcessIdentity returns the current OS identity of pid so a fresh
// parent can record it. It never signals the process.
func ObserveProcessIdentity(pid int) (string, error) { return observeProcessIdentity(pid) }

// ClassifyRecordedProcess answers whether the exact process recorded in the
// durable marker is still alive. It never signals, kills or reuses a PID number.
// A recorded identity that no longer matches proves the original child exited
// (the PID may have been reused by an unrelated process).
func ClassifyRecordedProcess(pid int, token string) ProcessState {
	if pid <= 0 || token == "" {
		return ProcessUnknown
	}
	current, err := observeProcessIdentity(pid)
	if errors.Is(err, errProcessGone) {
		return ProcessExited
	}
	if err != nil {
		return ProcessUnknown
	}
	if current == token {
		return ProcessAlive
	}
	return ProcessExited
}
