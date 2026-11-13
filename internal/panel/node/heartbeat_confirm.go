package node

import (
	"context"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// confirmDeploymentByHeartbeat settles a pending revision that the Agent
// already runs, for when its ApplyResult was lost with the session that held
// it (the Agent keeps results in session memory only). The Agent reports its
// committed manifest revision as successful_revision, and commits a manifest
// only after a revision applied and passed its health check, so a fresh
// heartbeat naming the pending revision with healthy cores is the same fact
// the lost result carried. Anything else, including a different revision, is
// left to the ApplyResult: failures, rollbacks and their codes come only from
// there. It returns the confirmed revision, or 0.
func (c *Control) confirmDeploymentByHeartbeat(ctx context.Context, nodeID string, heartbeat *pb.Heartbeat) int64 {
	if heartbeat.SuccessfulRevision < 1 || !heartbeat.GetCoreHealthy() {
		return 0
	}
	command, err := c.Deployment.Pending(ctx, nodeID)
	if err != nil || command == nil {
		return 0
	}
	state, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil || state.Revision != heartbeat.SuccessfulRevision {
		return 0
	}
	if err := c.Deployment.Result(ctx, nodeID, &pb.ApplyResult{Revision: state.Revision, Success: true, Code: "success"}); err != nil {
		if c.Logger != nil {
			c.Logger.Warn("deployment confirmation by heartbeat failed", "node_id", nodeID, "revision", state.Revision, "error", err)
		}
		return 0
	}
	if c.Logger != nil {
		c.Logger.Info("deployment confirmed by heartbeat", "node_id", nodeID, "revision", state.Revision)
	}
	return state.Revision
}
