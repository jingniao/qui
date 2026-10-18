package system

import (
	"context"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/restoretasks"
)

func (s *BackupService) RestoreRecords(ctx context.Context, page, size int) ([]restoretasks.Record, int, error) {
	if page < 1 || size < 1 || size > 100 {
		return nil, 0, restoretasks.ErrStore
	}
	if err := s.Source.CheckConfigurationKey(); err != nil {
		return nil, 0, err
	}
	materials, err := os.ReadFile(filepath.Join(s.Source.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		return nil, 0, err
	}
	defer clear(materials)
	records, err := (restoretasks.Store{Directory: filepath.Join(filepath.Dir(s.Source.Config.Database), "restore-progress"), Key: materials}).List(ctx)
	if err != nil {
		return nil, 0, err
	}
	total := len(records)
	start := total
	if page-1 <= total/size {
		start = min((page-1)*size, total)
	}
	end := min(start+size, total)
	return records[start:end], total, nil
}
