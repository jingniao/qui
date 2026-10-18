package collector

import (
	"math"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func (r *Runtime) increment(counter *atomic.Uint64) {
	for {
		previous := counter.Load()
		if previous == math.MaxUint64 {
			r.saturated.Store(true)
			return
		}
		if counter.CompareAndSwap(previous, previous+1) {
			return
		}
	}
}
func (r *Runtime) lostObservation(allowed bool) {
	r.dropped.Add(1)
	if allowed {
		r.increment(&r.lostAllowed)
	} else {
		r.increment(&r.lostBlocked)
	}
}

// A process-local observation; persistent spool evictions are separate facts.
// All reads are nonblocking, including aggregation length during disk flush.
func (r *Runtime) Status() *pb.CollectorStatus {
	result := &pb.CollectorStatus{ProcessId: r.processID, Status: "ok", StartedAt: timestamppb.New(r.startedAt), ObservedAt: timestamppb.New(time.Now().UTC()), PendingObservations: uint64(len(r.incoming)), AggregatedRollups: uint64(r.aggregator.Len()), DroppedAllowedObservations: r.lostAllowed.Load(), DroppedBlockedObservations: r.lostBlocked.Load(), RejectedObservations: r.unattributed.Load(), PersistenceFailures: r.persistenceFailures.Load(), CountersSaturated: r.saturated.Load()}
	if attempt := r.lastFlush.Load(); attempt > 0 {
		result.LastFlushAt = timestamppb.New(time.Unix(0, attempt).UTC())
	}
	if success := r.lastFlushSuccess.Load(); success > 0 {
		result.LastFlushSuccessAt = timestamppb.New(time.Unix(0, success).UTC())
	}
	if result.DroppedAllowedObservations > 0 || result.DroppedBlockedObservations > 0 || result.RejectedObservations > 0 || result.CountersSaturated {
		result.Status = "observation_loss"
	}
	if r.persistenceFailed.Load() {
		result.Status = "persistence_degraded"
	}
	result.PersistedLoss = r.lossStatus.Load()
	if result.PersistedLoss == nil {
		result.PersistedLoss = &pb.CollectorLossStatus{Status: "unavailable", CoverageIncomplete: true}
	}
	return result
}
