package supervisor

import (
	"context"
	"log/slog"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"sync"
	"time"
)

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
			for {
				select {
				case <-ctx.Done():
					return
				case <-probeTicker.C:
					if reconciler.CoreUpdateHeld() {
						continue
					}
					if err := prober.Probe(ctx); err != nil {
						logger.Warn("outbound health probe failed")
					}
				}
			}
		}()
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	var backoff restoreBackoff
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reconciler.Current() == 0 || reconciler.CoreUpdateHeld() {
				continue
			}
			if changed, err := reconciler.Refresh(ctx); err != nil {
				logger.Error("approved runtime state reconciliation failed", "error", err)
			} else if changed {
				logger.Info("approved runtime state applied", "revision", reconciler.Current())
			}
			health, err := adapter.Health(ctx)
			if err == nil && health.Healthy {
				backoff.reset()
				continue
			}
			if !backoff.ready(reconciler.Current(), time.Now()) {
				continue
			}
			logger.Warn("core unhealthy; restoring last successful configuration")
			if err := reconciler.Restore(ctx); err != nil {
				if backoff.failed(err, time.Now()) {
					logger.Error("core recovery failed", "error", err, "retry_in", backoff.delay)
				}
			} else {
				backoff.reset()
			}
		}
	}
}
