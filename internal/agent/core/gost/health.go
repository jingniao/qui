package gost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"proxycontrol/internal/agent/core"
)

// settleWindow is how long a freshly started child must stay alive with every
// rendered listener before the revision counts as applied. GOST reports a fatal
// configuration it refused within tens of milliseconds, and its first listener was
// measured ready after 12 ms, so two seconds covers a real start without delaying
// an apply noticeably.
const settleWindow = 2 * time.Second

// settle is the second validation stage. GOST has no config check: an unknown
// handler, a duplicate service name or an occupied port surfaces only when the
// process serves, and the failure is one fatal line plus exit status 1. The child
// itself is therefore the semantic validator, and it has to survive the window with
// its own TCP listeners answering and its UDP sockets bound.
func settle(ctx context.Context, pid int, done <-chan error, configuration core.RenderedConfig, metricsSocket string, childLog func() string) error {
	deadline := time.Now().Add(settleWindow)
	var last error
	for {
		select {
		case exit := <-done:
			reason := childLog()
			if exit == nil {
				exit = errors.New("exited during startup")
			}
			if reason != "" {
				return fmt.Errorf("gost %w: %s", exit, reason)
			}
			return fmt.Errorf("gost %w", exit)
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		last = listenerHealth(configuration, pid)
		if last == nil {
			if err := metricsReady(metricsSocket); err != nil {
				last = err
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if last != nil {
		reason := childLog()
		if reason != "" {
			return fmt.Errorf("%w: %s", last, reason)
		}
		return last
	}
	return nil
}

// listenerHealth checks what the post-change health check checks: every rendered
// TCP listener answers a dial and every rendered UDP socket is bound. A failure
// names the listener and wraps core.ErrListenerUnavailable, so the supervisor waits
// for consecutive failures instead of restarting a core that may still serve.
func listenerHealth(configuration core.RenderedConfig, pid int) error {
	for _, endpoint := range configuration.HealthEndpoints {
		connection, err := net.DialTimeout("tcp", endpoint, 500*time.Millisecond)
		if err != nil {
			return fmt.Errorf("gost tcp listener %s unavailable: %w: %w", endpoint, core.ErrListenerUnavailable, err)
		}
		connection.Close()
	}
	for _, endpoint := range configuration.UDPHealthEndpoints {
		if !core.UDPEndpointHealthy(endpoint, pid) {
			return fmt.Errorf("gost udp socket %s unavailable: %w", endpoint, core.ErrListenerUnavailable)
		}
	}
	return nil
}

// metricsReady requires the private metrics socket of the new process. It belongs to
// the settle gate only: a scrape failure later is a traffic-source status, not
// evidence that the forwards are broken.
func metricsReady(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return errors.New("gost metrics socket is not ready")
	}
	return nil
}

// Health answers the supervisor. GOST health is the exact child being alive, every
// production TCP listener answering a dial and every production UDP socket being
// bound; it is not a test of whether the forward's targets are reachable, and it
// never writes probe bytes to them.
func (a *Adapter) Health(ctx context.Context) (core.HealthStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := core.HealthStatus{CheckedAt: time.Now().UTC()}
	if a.process == nil {
		if err := a.lifecycleReadyLocked(); err != nil {
			return result, err
		}
		return result, errors.New("core is not running")
	}
	select {
	case exit := <-a.done:
		a.process, a.done, a.stopPending = nil, nil, false
		if err := a.marker.ConfirmExit(); err != nil {
			return result, errors.Join(core.ErrCoreStopUnconfirmed, err)
		}
		// The child's own exit result is the only explanation of why it is gone.
		if exit != nil {
			return result, fmt.Errorf("core exited: %w", exit)
		}
		return result, errors.New("core exited")
	default:
	}
	if err := listenerHealth(a.current, a.process.Process.Pid); err != nil {
		return result, err
	}
	result.Healthy = true
	return result, nil
}
