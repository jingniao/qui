package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"sync"
	"time"
)

// Each tick health-checks the core before refreshing the runtime state. A failed
// startup restore leaves the rendered digest empty, so refreshing first would
// render and apply the retained revision on every tick: a revision the installed
// core cannot render would log a failure every three seconds, and one that
// renders but cannot start (a taken port, for instance) would be applied again
// and again, so the restore backoff below would never take effect. An unhealthy
// core therefore goes through the backoff and Restore, which re-renders the
// current revision itself, and only a healthy core is refreshed.
//
// The unhealthy-core loop shares the refresh tick. A restore that keeps failing
// the same way (a retained revision the installed core cannot run) must not be
// retried every three seconds, so consecutive failures back off instead. A core
// whose listener did not answer is checked again on the next tick instead of
// being restarted at once; see coreRecovery. The schedule is a variable so a
// test can compress it.
var (
	tickInterval     = 3 * time.Second
	restoreRetryBase = 3 * time.Second
	restoreRetryMax  = time.Minute
)

// restoreBackoff delays the next restore attempt after consecutive failures.
type restoreBackoff struct {
	revision int64
	delay    time.Duration
	due      time.Time
	message  string
	capped   bool
}

// ready reports whether the next attempt is due now. A different current
// revision starts over, so a revision the adapter can run is never delayed.
func (b *restoreBackoff) ready(revision int64, now time.Time) bool {
	if b.revision != revision {
		b.reset()
		b.revision = revision
	}
	return !now.Before(b.due)
}

func (b *restoreBackoff) reset() { b.delay, b.due, b.message, b.capped = 0, time.Time{}, "", false }

// failed extends the wait after a failed attempt and reports whether the failure
// should be logged: the error text changed, or the wait reached the cap.
func (b *restoreBackoff) failed(err error, now time.Time) bool {
	if b.delay == 0 {
		b.delay = restoreRetryBase
	} else {
		b.delay *= 2
		if b.delay > restoreRetryMax {
			b.delay = restoreRetryMax
		}
	}
	b.due = now.Add(b.delay)
	message, capped := err.Error(), b.delay >= restoreRetryMax
	log := message != b.message || capped && !b.capped
	b.message, b.capped = message, capped
	return log
}

// coreRecovery is one core's supervision state: the restore backoff plus the
// streak of consecutive health checks that failed only because a listener did
// not answer.
//
// A dial that does not answer says nothing about the core that is still serving:
// a full conntrack table drops the SYN, and the restore itself restarts the core
// and drops every live connection, which makes the overload worse. A
// listener-only failure therefore has to repeat listenerFailureLimit times before
// the core is restored. Any other failure, including an unhealthy result an
// adapter reports without a reason, still restores on the first check.
type coreRecovery struct {
	backoff        restoreBackoff
	listenerStreak int
	revision       int64
}

// listenerFailureLimit is the number of consecutive listener-only health checks
// that justify restarting a core.
const listenerFailureLimit = 3

// observe records one health result of revision and reports whether the core is
// to be restored now. A failed restore keeps the streak, so the retries that
// follow keep the backoff instead of waiting for three fresh failures.
func (c *coreRecovery) observe(revision int64, healthy bool, err error) bool {
	if c.revision != revision {
		c.revision, c.listenerStreak = revision, 0
	}
	if healthy {
		c.reset()
		return false
	}
	if !errors.Is(err, core.ErrListenerUnavailable) {
		c.listenerStreak = 0
		return true
	}
	if c.listenerStreak < listenerFailureLimit {
		c.listenerStreak++
	}
	return c.listenerStreak >= listenerFailureLimit
}

// reset starts the streak and the backoff over: the core answered its check, a
// restore brought it back, or an update or revision the failures predate owns it.
func (c *coreRecovery) reset() {
	c.listenerStreak = 0
	c.backoff.reset()
}

func Run(ctx context.Context, adapter core.Adapter, reconciler *reconcile.Reconciler, logger *slog.Logger) {
	var workers sync.WaitGroup
	defer workers.Wait()
	if source, ok := adapter.(interface{ SampleConnections(context.Context) }); ok {
		workers.Add(1)
		go func() {
			defer workers.Done()
			// Fast enough to catch a source between a connection closing and the
			// core forgetting it; the store keeps what one sample saw.
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				source.SampleConnections(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	if target, ok := adapter.(interface{ TickDeviceGates(context.Context) error }); ok {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					var gateErr error
					if multi, ok := target.(interface {
						TickDeviceGatesExcept(context.Context, map[string]bool) error
					}); ok {
						gateErr = multi.TickDeviceGatesExcept(ctx, map[string]bool{"xray": reconciler.CoreComponentHeld("xray"), "sing-box": reconciler.CoreComponentHeld("sing-box")})
					} else if !reconciler.CoreUpdateHeld() {
						gateErr = target.TickDeviceGates(ctx)
					}
					if gateErr != nil {
						logger.Warn("device gate failed open; routing recovery may be required")
					}
				}
			}
		}()
	}
	if prober, ok := adapter.(interface{ Probe(context.Context) error }); ok {
		workers.Add(1)
		go func() {
			defer workers.Done()
			probeTicker := time.NewTicker(3 * time.Second)
			defer probeTicker.Stop()
			// The probe usually reports the same result for minutes, so only a
			// change of that result is logged; a line every three seconds would
			// add nothing.
			failing := false
			for {
				select {
				case <-ctx.Done():
					return
				case <-probeTicker.C:
					var probeErr error
					if multi, ok := prober.(interface {
						ProbeExcept(context.Context, map[string]bool) error
					}); ok {
						probeErr = multi.ProbeExcept(ctx, map[string]bool{"xray": reconciler.CoreComponentHeld("xray"), "sing-box": reconciler.CoreComponentHeld("sing-box")})
					} else if !reconciler.CoreUpdateHeld() {
						probeErr = prober.Probe(ctx)
					}
					if err := probeErr; err != nil {
						if !failing {
							logger.Warn("outbound health probe failed", "error", err)
							failing = true
						}
					} else if failing {
						failing = false
						logger.Info("outbound health probe recovered")
					}
				}
			}
		}()
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	// recoveries holds one state per core name a health check reports, or one for
	// the adapter name of a single-core adapter.
	recoveries := map[string]*coreRecovery{}
	recovery := func(name string) *coreRecovery {
		state := recoveries[name]
		if state == nil {
			state = &coreRecovery{}
			recoveries[name] = state
		}
		return state
	}
	// refreshFailure is the reason the last refresh failed, so the same transient
	// failure is not logged on every tick.
	refreshFailure := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reconciler.Current() == 0 {
				continue
			}
			if multi, ok := adapter.(interface {
				CoreHealths(context.Context) []core.CoreHealth
			}); ok {
				unhealthy := false
				reported := map[string]bool{}
				for _, item := range multi.CoreHealths(ctx) {
					status := item.Status
					reported[status.Name] = true
					state := recovery(status.Name)
					if reconciler.CoreComponentHeld(status.Name) {
						// An update owns this core: the checks after it must
						// not count the failures from before it.
						state.reset()
						unhealthy = true
						continue
					}
					if status.Healthy {
						state.observe(reconciler.Current(), true, nil)
						continue
					}
					// A listener that did not answer is still an unhealthy
					// core: a refresh would re-apply a revision it cannot
					// serve.
					unhealthy = true
					now := time.Now()
					if !state.observe(reconciler.Current(), false, item.Err) || !state.backoff.ready(reconciler.Current(), now) {
						continue
					}
					if err := reconciler.RestoreCore(ctx, status.Name); err != nil {
						if state.backoff.failed(err, now) {
							logger.Error("core recovery failed", "core", status.Name, "error", err, "retry_in", state.backoff.delay.String())
						}
					} else {
						state.reset()
						logger.Warn("core unhealthy; restored last successful configuration", "core", status.Name, "error", item.Err, "revision", reconciler.Current())
					}
				}
				for name := range recoveries {
					if !reported[name] {
						delete(recoveries, name)
					}
				}
				if unhealthy {
					continue
				}
				// Every core answered its own check. Sampling the aggregate
				// health here would ask a second time, and a stale second
				// sample would restore the whole runtime.
				//
				// A hold no reported core owns, such as a core being added
				// that CoreHealths does not report yet, still holds the
				// runtime: the refresh refuses every hold, so asking it would
				// log that refusal at Error on every tick.
				if reconciler.CoreUpdateHeld() {
					continue
				}
			} else {
				state := recovery(adapter.Name())
				if reconciler.CoreUpdateHeld() {
					state.reset()
					continue
				}
				// Health comes first: while the core cannot run the retained
				// revision, a refresh would only re-render and re-apply it.
				health, err := adapter.Health(ctx)
				if err == nil && !health.Healthy {
					// Every restore must name its reason, even when an
					// adapter reports unhealthy without one.
					err = fmt.Errorf("%s: unhealthy", adapter.Name())
				}
				if err != nil {
					now := time.Now()
					if !state.observe(reconciler.Current(), false, err) || !state.backoff.ready(reconciler.Current(), now) {
						continue
					}
					logger.Warn("core unhealthy; restoring last successful configuration", "error", err, "revision", reconciler.Current())
					if err := reconciler.Restore(ctx); err != nil {
						if state.backoff.failed(err, now) {
							logger.Error("core recovery failed", "error", err, "retry_in", state.backoff.delay.String())
						}
					} else {
						state.reset()
					}
					continue
				}
				state.observe(reconciler.Current(), true, nil)
			}
			changed, err := reconciler.Refresh(ctx)
			if err != nil {
				// A normal stop cancels the refresh; that is not a failure.
				if ctx.Err() != nil {
					return
				}
				if message := err.Error(); message != refreshFailure {
					logger.Error("approved runtime state reconciliation failed", "error", err)
					refreshFailure = message
				}
				continue
			}
			if refreshFailure != "" {
				refreshFailure = ""
				logger.Info("approved runtime state reconciliation recovered")
			}
			if changed {
				logger.Info("approved runtime state applied", "revision", reconciler.Current())
			}
		}
	}
}
