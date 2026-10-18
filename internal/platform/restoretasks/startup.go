package restoretasks

import (
	"context"
	"time"
)

// Capture the exact original start intent before entering the service. A
// later record cannot be silently rebound to this callback after a restart.
func (s Store) StartupRecorder(ctx context.Context, identifier string) (func(context.Context) error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	original, err := s.read(identifier)
	if err != nil {
		return nil, err
	}
	if original.Status != "active" || original.LastStage != "serving" || original.RuntimeEnteredAt != "" {
		return nil, ErrStore
	}
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := s.read(identifier)
		if err != nil {
			return err
		}
		if current != original || current.Sequence >= (1<<62)-1 {
			return ErrStore
		}
		now := time.Now().UTC()
		previous, e := time.Parse(time.RFC3339Nano, current.UpdatedAt)
		if e != nil || now.Before(previous) {
			return ErrStore
		}
		current.Sequence++
		current.RuntimeEnteredAt = now.Format(time.RFC3339Nano)
		current.UpdatedAt = current.RuntimeEnteredAt
		return s.write(ctx, current, false, original)
	}, nil
}
func (s Store) FinishServing(ctx context.Context, identifier string, serveErr error) error {
	end, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	stage, status, code := "stopped", "stopped", ""
	if serveErr != nil {
		stage, status, code = "serving", "failed", "restore_failed"
	}
	_, err := s.Advance(end, identifier, stage, status, code)
	return err
}
