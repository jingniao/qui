package server

import (
	"context"
	"time"

	"proxycontrol/internal/panel/deployment"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// Control asks for pending work after persisting capabilities. Reconcile here
// so a reconnect can queue cleanup before the next command is read.
type relayCleanupControl struct {
	*deployment.Repository
	cleanup *deployment.RelayCleanupDeployer
}

func (c *relayCleanupControl) Pending(ctx context.Context, nodeID string) (*pb.ApplyCommand, error) {
	if err := c.cleanup.ReconcileConnectedNode(ctx, nodeID); err != nil {
		return nil, err
	}
	return c.Repository.Pending(ctx, nodeID)
}

func (s *Server) runRelayCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.relayCleanup.Reconcile(ctx); err != nil && ctx.Err() == nil {
			s.Logger.Error("relay cleanup reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
