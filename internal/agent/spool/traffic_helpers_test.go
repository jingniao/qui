package spool

import (
	"context"
	"proxycontrol/pkg/contracts/traffic"
)

// PendingTrafficRetry is PendingTraffic with the quarantine flag of the
// delivered batch, so the caller can report the retry after the cool-down.
func (s *Spool) PendingTrafficRetry(ctx context.Context) (*traffic.Batch, bool, error) {
	return s.pendingTraffic(ctx)
}
