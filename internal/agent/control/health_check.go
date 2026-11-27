package control

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func reportCoreHealth(ctx context.Context, heartbeat *pb.Heartbeat, manager *reconcile.Reconciler, coreName string) {
	if source, ok := manager.Adapter.(interface {
		CoreStatuses(context.Context) []*pb.CoreStatus
	}); ok {
		heartbeat.Cores = source.CoreStatuses(ctx)
	}
	status, err := manager.Health(ctx)
	startupFailed := manager.StartupError() != nil || errors.Is(err, reconcile.ErrRuntimeRestore)
	stopUnconfirmed := errors.Is(err, core.ErrCoreStopUnconfirmed)
	if startupFailed || stopUnconfirmed || heartbeat.CurrentRevision > 0 && (len(heartbeat.Cores) > 0 || coreName != "") {
		healthy := err == nil && status.Healthy
		heartbeat.CoreHealthy = &healthy
		if !healthy {
			heartbeat.CoreError = "core_unhealthy"
			if stopUnconfirmed {
				heartbeat.CoreError = "core_lifecycle_unconfirmed"
			}
		}
	}
	if startupFailed {
		for _, item := range heartbeat.Cores {
			item.Healthy = false
			item.Error = heartbeat.CoreError
		}
	}
	if len(heartbeat.Cores) == 0 && coreName != "" {
		heartbeat.Cores = []*pb.CoreStatus{{Name: coreName, Healthy: heartbeat.GetCoreHealthy(), Error: heartbeat.CoreError, TrafficSourceStatus: heartbeat.TrafficSourceStatus}}
	}
}

func performHealthCheck(ctx context.Context, command *pb.HealthCheckCommand, revision int64, source any, supported bool) *pb.HealthCheckResult {
	result := &pb.HealthCheckResult{Id: command.Id, Core: command.Core, Revision: command.Revision, Code: "unsupported", ObservedAt: timestamppb.Now()}
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
	if command.Core != "" {
		selector, ok := source.(interface{ Component(string) core.Runtime })
		if !ok || selector.Component(command.Core) == nil {
			return result
		}
		source = selector.Component(command.Core)
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
