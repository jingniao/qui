package supervisor

import (
	"context"
	"log/slog"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"sync"
	"time"
)

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
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
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
				continue
			}
			logger.Warn("core unhealthy; restoring last successful configuration")
			if err := reconciler.Restore(ctx); err != nil {
				logger.Error("core recovery failed", "error", err)
			}
		}
	}
}
