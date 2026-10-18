package control

import (
	"context"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/core"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

func performHealthCheck(ctx context.Context, command *pb.HealthCheckCommand, revision int64, source any, supported bool) *pb.HealthCheckResult {
	result := &pb.HealthCheckResult{Id: command.Id, Revision: command.Revision, Code: "unsupported", ObservedAt: timestamppb.Now()}
	if !supported {
		return result
	}
	if command.Id == "" || len(command.Id) > 128 || command.Revision < 1 || command.Deadline == nil || command.Deadline.CheckValid() != nil || time.Now().After(command.Deadline.AsTime()) {
		result.Code = "expired"
		return result
	}
	if command.Revision != revision {
		result.Code = "revision_changed"
		return result
	}
	health, ok := source.(interface {
		Health(context.Context) (core.HealthStatus, error)
	})
	if !ok {
		return result
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	observed, err := health.Health(check)
	result.Code = "core_unhealthy"
	result.Healthy = err == nil && observed.Healthy
	if result.Healthy {
		result.Code = "healthy"
	}
	result.ObservedAt = timestamppb.Now()
	return result
}
