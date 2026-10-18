package node

import (
	"errors"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

func validateCollectorLossStatus(s *pb.CollectorLossStatus, collector *pb.CollectorStatus) error {
	if s == nil {
		return nil
	}
	if !s.CoverageIncomplete {
		return errors.New("collector checkpoint cannot prove complete historical coverage")
	}
	if !s.Measured {
		if s.Status != "unavailable" || s.Generation != 0 || s.DroppedAllowedObservations != 0 || s.DroppedBlockedObservations != 0 || s.RejectedObservations != 0 || s.PersistenceFailures != 0 || s.UnconfirmedProcesses != 0 || s.BaselineAt != nil || s.CheckpointAt != nil || s.CountersSaturated || s.EndConfirmed {
			return errors.New("unavailable collector checkpoint cannot declare counters")
		}
		return nil
	}
	if s.Status != "ok" || s.Generation == 0 || s.UnconfirmedProcesses >= s.Generation || s.BaselineAt == nil || s.CheckpointAt == nil || s.BaselineAt.CheckValid() != nil || s.CheckpointAt.CheckValid() != nil || s.BaselineAt.AsTime().After(s.CheckpointAt.AsTime()) || s.CheckpointAt.AsTime().After(collector.ObservedAt.AsTime().Add(time.Second)) {
		return errors.New("invalid persisted collector loss observation")
	}
	return nil
}
