package core

import (
	"errors"
	"io"
	"os"
	"proxycontrol/internal/platform/filelock"
)

// Absence is established with Lstat, never by following a broken symlink.
// Existing invalid material is not converted into a fresh accounting run.
func readAccountingMaterial(path string, maximum int64, exact bool) (data []byte, resultErr error) {
	return readAccountingMaterialLinks(path, maximum, exact, false)
}

// The only publication pair exception is immutable completed-run history;
// active journals and keys always require one link.
func readAccountingMaterialLinks(path string, maximum int64, exact, historyPair bool) (data []byte, resultErr error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum || exact && info.Size() != maximum {
		return nil, errors.New("accounting material is not an original bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		// The name existed at Lstat. A failed open must not become absence.
		return nil, errors.New("existing accounting material cannot be opened")
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			clear(data)
			data = nil
		}
	}()
	opened, err := file.Stat()
	checkLinks := func() error {
		if err := filelock.CheckSingleLink(file); err != nil {
			if historyPair {
				return filelock.CheckLinkCount(file, 2)
			}
			return err
		}
		return nil
	}
	if err != nil || !os.SameFile(info, opened) || checkLinks() != nil {
		return nil, errors.New("accounting material identity or links unavailable")
	}
	data, err = io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) != info.Size() || int64(len(data)) > maximum {
		clear(data)
		return nil, errors.New("accounting material size changed or read failed")
	}
	current, err := os.Lstat(path)
	after, statErr := file.Stat()
	if err != nil || statErr != nil || !os.SameFile(opened, current) || current.Size() != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || checkLinks() != nil {
		clear(data)
		return nil, errors.New("accounting material changed while reading")
	}
	return data, nil
}
