package core

import (
	"context"
	"errors"
	access "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

var ErrReleaseRecovery = errors.New("core release recovery must finish before runtime reconciliation")

// StopReapTimeout bounds how long a forced core stop waits for the killed
// child's exit to be observed before reporting an unconfirmed stop.
const StopReapTimeout = 5 * time.Second

// ErrCoreStopUnconfirmed reports that a killed child core's exit was not observed
// in time. The adapter retains the original process and accounting run so that no
// replacement is started and no consumption is written off.
var ErrCoreStopUnconfirmed = errors.New("core process exit is unconfirmed; retain the original process and do not start a replacement")

// ErrListenerUnavailable marks a health check that failed only because a core
// listener did not answer. One unanswered dial is not evidence that a running
// core is broken (a full conntrack table drops SYNs, for instance), so the
// supervisor waits for consecutive listener failures before it restarts the
// core, which would drop every live connection. Every other health failure
// still restores at once.
var ErrListenerUnavailable = errors.New("core listener did not answer")

// ReapProcess waits up to timeout for a killed child's exit to be observed. It
// returns nil when the exit was observed, or ErrCoreStopUnconfirmed otherwise.
func ReapProcess(done <-chan error, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return ErrCoreStopUnconfirmed
	}
}

type RuntimeHooks struct {
	TrafficSink    func(context.Context, []traffic.Counter) error
	AccessSink     func(access.Rollup) error
	AccessRejected func()
	EnableStats    bool
}

type Runtime interface {
	Adapter
	ConfigureRuntime(RuntimeHooks)
	Close() error
	SampleTraffic(context.Context) error
	SupportsAccountTelemetry() bool
	OutboundHealth() map[string]scheduler.Health
}

// BinaryRuntime selects only the fixed adapter's executable after its process
// has stopped. Reconciler serializes the entire update against publication,
// policy refresh and ordinary rollback.
type BinaryRuntime interface {
	Runtime
	ExecutablePath() string
	DetectVersion(context.Context, string) (string, error)
	ValidateExecutable(context.Context, string, RenderedConfig) (string, error)
	SelectExecutable(context.Context, string) error
}
