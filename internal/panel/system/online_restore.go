package system

import (
	"context"
	"errors"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/restoretasks"
)

type OnlineRestore struct {
	Target  archive.RestoreTarget
	Reserve func() (func(), error)
	Accept  func(context.Context, archive.PreparedRestore, []byte, string, string) (restoretasks.Record, func(), error)
}

func (s *BackupService) PrepareOnlineRestore(ctx context.Context, backupID, password, expectedSHA string, target archive.RestoreTarget) (archive.PreparedRestore, error) {
	if err := archive.ValidatePassword(password); err != nil {
		return archive.PreparedRestore{}, err
	}
	done, err := s.beginStore(ctx)
	if err != nil {
		return archive.PreparedRestore{}, err
	}
	defer done()
	item, err := s.Repository.Backup(ctx, backupID)
	if err != nil {
		return archive.PreparedRestore{}, err
	}
	if item.SHA256 != expectedSHA {
		return archive.PreparedRestore{}, ErrBackupFile
	}
	file, err := s.openChecked(ctx, item)
	if err != nil {
		return archive.PreparedRestore{}, err
	}
	file.Close()
	prepared, err := archive.PrepareRestore(ctx, filepath.Join(filepath.Dir(s.Source.Config.Database), "restore-staging"), filepath.Join(s.Directory, item.ID+".pcb"), password, target)
	if err != nil {
		return prepared, err
	}
	if prepared.Plan.Source.ID != item.ID || prepared.Plan.ArchiveSHA256 != expectedSHA {
		return archive.PreparedRestore{}, errors.Join(ErrBackupFile, archive.DiscardPreparedRestore(context.Background(), prepared))
	}
	return prepared, nil
}
