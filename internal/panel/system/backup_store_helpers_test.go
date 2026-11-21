package system

import (
	"context"
)

// Recover owns the process lock; it never reconstructs catalog rows from files.
func (s *BackupService) Recover(ctx context.Context) error {
	done, err := s.beginStore(ctx)
	if err != nil {
		return err
	}
	defer done()
	return nil
}
