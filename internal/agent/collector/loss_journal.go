package collector

import (
	"context"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/spool"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// Only Run writes checkpoints. Heartbeat reads an immutable atomic snapshot,
// never performing a SQLite query or advancing diagnostic/billing state.
func (r *Runtime) persistLoss(ctx context.Context, closed bool) {
	failed := func() {
		r.lossStatus.Store(&pb.CollectorLossStatus{Status: "unavailable", CoverageIncomplete: true})
		r.logger.Warn("collector loss checkpoint unavailable; process counters remain observable")
	}
	if r.lossGeneration == 0 {
		state, err := r.queue.BeginCollectorLoss(ctx, r.processID, r.startedAt)
		if err != nil {
			failed()
			return
		}
		r.lossGeneration = state.Generation
	}
	counts := spool.CollectorLossCounts{Allowed: r.lostAllowed.Load(), Blocked: r.lostBlocked.Load(), Rejected: r.unattributed.Load(), PersistenceFailures: r.persistenceFailures.Load()}
	state, err := r.queue.CheckpointCollectorLoss(ctx, r.processID, r.lossGeneration, counts, r.saturated.Load(), closed)
	if err != nil {
		failed()
		return
	}
	r.lossStatus.Store(&pb.CollectorLossStatus{Measured: true, Status: "ok", Generation: state.Generation, DroppedAllowedObservations: state.Total.Allowed, DroppedBlockedObservations: state.Total.Blocked, RejectedObservations: state.Total.Rejected, PersistenceFailures: state.Total.PersistenceFailures, UnconfirmedProcesses: state.UnconfirmedProcesses, BaselineAt: timestamppb.New(state.BaselineAt), CheckpointAt: timestamppb.New(state.CheckpointAt), CountersSaturated: state.Saturated, CoverageIncomplete: true, EndConfirmed: state.EndConfirmed})
}
