package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"sync/atomic"
	"time"
)

type Runtime struct {
	queue               *spool.Spool
	aggregator          *Aggregator
	incoming            chan access.Rollup
	done                chan struct{}
	dropped             atomic.Uint64
	rejected            atomic.Uint64
	logger              *slog.Logger
	processID           string
	startedAt           time.Time
	lostAllowed         atomic.Uint64
	lostBlocked         atomic.Uint64
	unattributed        atomic.Uint64
	persistenceFailures atomic.Uint64
	saturated           atomic.Bool
	persistenceFailed   atomic.Bool
	lastFlush           atomic.Int64
	lastFlushSuccess    atomic.Int64
	lossGeneration      uint64
	lossStatus          atomic.Pointer[pb.CollectorLossStatus]
}

func NewRuntime(queue *spool.Spool, logger *slog.Logger) (*Runtime, error) {
	if queue == nil {
		return nil, errors.New("collector requires durable spool")
	}
	if logger == nil {
		logger = slog.Default()
	}
	aggregator, err := NewAggregator(10000)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &Runtime{queue: queue, aggregator: aggregator, incoming: make(chan access.Rollup, 2048), done: make(chan struct{}), logger: logger, processID: hex.EncodeToString(id), startedAt: time.Now().UTC()}, nil
}

func (r *Runtime) Submit(record access.Rollup) error {
	select {
	case r.incoming <- record:
		return nil
	default:
		r.lostObservation(record.Allowed)
		return errors.New("telemetry collector backlog full")
	}
}
func (r *Runtime) Reject() { r.rejected.Add(1); r.increment(&r.unattributed) }
func (r *Runtime) Wait()   { <-r.done }
func (r *Runtime) flush(ctx context.Context) error {
	if r.aggregator.Len() == 0 {
		return nil
	}
	err := r.aggregator.Flush(time.Now().UTC().Add(time.Minute), func(records []access.Rollup) error {
		result, err := r.queue.Enqueue(ctx, records)
		if result.DroppedAllowedRollups > 0 {
			r.logger.Warn("telemetry spool evicted old allowed rollups", "count", result.DroppedAllowedRollups)
		}
		return err
	})
	r.lastFlush.Store(time.Now().UTC().UnixNano())
	r.persistenceFailed.Store(err != nil)
	if err != nil {
		r.increment(&r.persistenceFailures)
	} else {
		r.lastFlushSuccess.Store(time.Now().UTC().UnixNano())
	}
	return err
}

func (r *Runtime) Run(ctx context.Context) {
	defer close(r.done)
	initial, initialCancel := context.WithTimeout(ctx, 2*time.Second)
	r.persistLoss(initial, false)
	initialCancel()
	report := func() {
		if lost := r.dropped.Swap(0); lost > 0 {
			r.logger.Error("telemetry observations dropped due to capacity", "count", lost)
		}
		if rejected := r.rejected.Swap(0); rejected > 0 {
			r.logger.Warn("telemetry observations rejected during parsing attribution or submission", "count", rejected)
		}
	}
	defer report()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	consume := func(record access.Rollup) {
		if err := r.aggregator.Add(record); err != nil {
			if errors.Is(err, ErrCapacity) {
				flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				flushErr := r.flush(flushCtx)
				cancel()
				if flushErr == nil {
					err = r.aggregator.Add(record)
				}
			}
			if err != nil {
				r.lostObservation(record.Allowed)
			}
		}
	}
	for {
		select {
		case record := <-r.incoming:
			consume(record)
		case <-ticker.C:
			flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := r.flush(flushCtx)
			cancel()
			if err != nil {
				r.logger.Error("telemetry persistence failed; aggregation retained")
			}
			checkpoint, checkpointCancel := context.WithTimeout(ctx, 2*time.Second)
			r.persistLoss(checkpoint, false)
			checkpointCancel()
			report()
		case <-ctx.Done():
			for {
				select {
				case record := <-r.incoming:
					consume(record)
				default:
					flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					err := r.flush(flushCtx)
					cancel()
					if err != nil {
						r.logger.Error("telemetry shutdown persistence failed")
					}
					checkpoint, checkpointCancel := context.WithTimeout(context.Background(), 2*time.Second)
					r.persistLoss(checkpoint, err == nil && r.aggregator.Len() == 0 && len(r.incoming) == 0)
					checkpointCancel()
					return
				}
			}
		}
	}
}
