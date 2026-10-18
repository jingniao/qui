package control

import (
	"context"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/spool"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

func sampleSpoolStatus(ctx context.Context, queue *spool.Spool) *pb.SpoolStatus {
	result := &pb.SpoolStatus{Status: "unavailable", FailureCode: "queue_unavailable", ObservedAt: timestamppb.New(time.Now().UTC())}
	if queue == nil {
		return result
	}
	query, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	stats, err := queue.Stats(query)
	if err != nil {
		result.FailureCode = spoolFailureCode(err, "capacity_unavailable")
		return result
	}
	// A capacity measurement cannot imply the oldest replay records are readable.
	// Authenticate only the bounded heads; never remove or skip a damaged batch.
	if _, err := queue.Pending(query); err != nil {
		result.FailureCode = spoolFailureCode(err, "access_head_unavailable")
		if errors.Is(err, spool.ErrQueuedBatchInvalid) {
			result.FailureCode = "access_head_invalid"
		}
		return result
	}
	if _, err := queue.PendingTraffic(query); err != nil {
		result.FailureCode = spoolFailureCode(err, "traffic_head_unavailable")
		if errors.Is(err, spool.ErrQueuedBatchInvalid) {
			result.FailureCode = "traffic_head_invalid"
		}
		return result
	}
	if stats.Bytes < 0 || stats.DiskBytes < 0 || stats.DiskLimit <= 0 || stats.DatabaseBytes < 0 || stats.DatabaseLimit <= 0 || stats.Batches < 0 || stats.DroppedAllowedRollups < 0 {
		result.FailureCode = "capacity_invalid"
		return result
	}
	result.Measured = true
	result.FailureCode = ""
	result.PayloadBytes = uint64(stats.Bytes)
	result.DiskBytes = uint64(stats.DiskBytes)
	result.DiskLimit = uint64(stats.DiskLimit)
	result.DatabaseBytes = uint64(stats.DatabaseBytes)
	result.DatabaseLimit = uint64(stats.DatabaseLimit)
	result.PendingBatches = uint64(stats.Batches)
	result.DroppedAllowedRollups = uint64(stats.DroppedAllowedRollups)
	result.Status = "ok"
	if stats.DiskBytes >= stats.DiskLimit || stats.DatabaseBytes >= stats.DatabaseLimit {
		result.Status = "capacity_exceeded"
	} else if float64(stats.DiskBytes)/float64(stats.DiskLimit) >= 0.85 || float64(stats.DatabaseBytes)/float64(stats.DatabaseLimit) >= 0.85 {
		result.Status = "near_capacity"
	}
	result.ObservedAt = timestamppb.New(time.Now().UTC())
	return result
}

func spoolFailureCode(err error, fallback string) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "observation_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "observation_interrupted"
	}
	return fallback
}
