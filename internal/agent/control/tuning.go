package control

import (
	"context"
	"fmt"
	"time"

	"proxycontrol/internal/agent/capability"
	"proxycontrol/internal/agent/tuning"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func reportCapabilities(s *capability.Sampler, c *capabilityState) *pb.Capabilities {
	result := c.Snapshot()
	result.Host = s.HostInfo()
	result.TuningSupported = tuning.Supported()
	return result
}

// One in-flight request across sessions. Neither a reconnect nor a second
// command may overwrite the desired file while a previous helper is running.
var tuningSlot = make(chan struct{}, 1)

func applyTuning(ctx context.Context, c *pb.TuningCommand) *pb.TuningResult {
	return runTuning(ctx, c, tuning.Directory, "/proc/sys", tuning.Supported())
}
func runTuning(ctx context.Context, c *pb.TuningCommand, directory, proc string, supported bool) *pb.TuningResult {
	result := &pb.TuningResult{RequestId: c.RequestId, Status: "failed"}
	fail := func(err error) *pb.TuningResult {
		result.Keys = []*pb.TuningKeyResult{{Error: err.Error()}}
		return result
	}
	if !supported {
		return fail(fmt.Errorf("tuning helper is not installed"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	desired := tuning.Desired{RequestID: c.RequestId, Settings: c.Settings, Clear: c.Clear}
	if err := desired.Validate(proc); err != nil {
		return fail(err)
	}
	select {
	case tuningSlot <- struct{}{}:
		defer func() { <-tuningSlot }()
	default:
		return fail(fmt.Errorf("tuning request in progress"))
	}
	if err := tuning.WriteDesired(directory, desired); err != nil {
		return fail(err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if status, err := tuning.ReadStatus(directory); err == nil && status.RequestId == c.RequestId {
			return status
		}
		select {
		case <-wait.Done():
			return fail(fmt.Errorf("tuning status unconfirmed: %w", wait.Err()))
		case <-ticker.C:
		}
	}
}
