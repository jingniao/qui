package server

import (
	"context"
	"errors"
	"time"

	"proxycontrol/internal/platform/db"
)

// Reclamation has its own timer so a retention/compaction failure cannot gate
// SQLite housekeeping or change the existing one-minute cleanup error retry.
// Both workers share shutdown/restore lifecycle and the serialized write pool.
func (s *Server) runSpaceMaintenance(ctx context.Context) {
	for ctx.Err() == nil {
		pending, err := s.reclaimFreePages(ctx)
		if err != nil && ctx.Err() == nil {
			s.Logger.Error("SQLite free page reclamation failed; committed cleanup progress is preserved", "error", err)
		}
		// Check idle databases each minute: cleanup can free more pages after
		// this worker's last pass without having to share its wake channel.
		delay := time.Minute
		if pending {
			delay = time.Second
		}
		if err != nil {
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Server) reclaimFreePages(ctx context.Context) (bool, error) {
	begin, beginCancel := context.WithTimeout(ctx, 2*time.Second)
	attempt, err := s.accessRepository.BeginSpaceMaintenance(begin)
	beginCancel()
	if err != nil {
		return false, err
	}
	mode, pending, reclaimErr := db.ReclaimFreePages(ctx, s.Database)
	// Preserve the attempt result during shutdown. If recording fails, the
	// durable running marker remains unconfirmed; retain both original errors.
	record, recordCancel := context.WithTimeout(context.Background(), 2*time.Second)
	recordErr := s.accessRepository.FinishSpaceMaintenance(record, attempt, mode, reclaimErr != nil)
	recordCancel()
	return pending, errors.Join(reclaimErr, recordErr)
}
