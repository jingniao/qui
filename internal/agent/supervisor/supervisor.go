package supervisor

import (
	"context"
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
// retried every three seconds, so consecutive failures back off instead. They
// are variables so a test can compress the schedule.
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

func Run(ctx context.Context, adapter core.Adapter, reconciler *reconcile.Reconciler, logger *slog.Logger) {
	var workers sync.WaitGroup
	defer workers.Wait()
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
					if reconciler.CoreUpdateHeld() {
						continue
					}
					if err := prober.Probe(ctx); err != nil {
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
	var backoff restoreBackoff
	// refreshFailure is the reason the last refresh failed, so the same transient
	// failure is not logged on every tick.
	refreshFailure := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reconciler.Current() == 0 || reconciler.CoreUpdateHeld() {
				continue
			}
			// Health comes first: while the core cannot run the retained
			// revision, a refresh would only re-render and re-apply it.
			health, err := adapter.Health(ctx)
			if err != nil || !health.Healthy {
				if !backoff.ready(reconciler.Current(), time.Now()) {
					continue
				}
				logger.Warn("core unhealthy; restoring last successful configuration")
				if err := reconciler.Restore(ctx); err != nil {
					if backoff.failed(err, time.Now()) {
						logger.Error("core recovery failed", "error", err, "retry_in", backoff.delay.String())
					}
				} else {
					backoff.reset()
				}
				continue
			}
			backoff.reset()
			changed, err := reconciler.Refresh(ctx)
			if err != nil {
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
