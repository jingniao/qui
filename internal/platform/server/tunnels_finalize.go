package server

import (
	"context"
	"time"
)

func (s *Server) runTunnelFinalization(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.tunnelPublications.ReconcileTunnelPublications(ctx, false); err != nil && ctx.Err() == nil {
			s.Logger.Error("tunnel publication finalization failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.tunnelFinalizeWake:
		case <-ticker.C:
		}
	}
}
