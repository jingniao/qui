package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/filelock"
)

func TryExisting(directory string) (*Ownership, error) { return tryExisting(directory, lockName) }
func TryExistingSupervisor(directory string) (*Ownership, error) {
	return tryExisting(directory, ".agent-supervisor.lock")
}
func tryExisting(directory, filename string) (_ *Ownership, resultErr error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	owner := &Ownership{directory: root}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owner.Close())
		}
	}()
	for _, path := range []string{root, filepath.Join(root, "identity")} {
		before, err := os.Lstat(path)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("original lifecycle directory required")
		}
		lock, err := filelock.TryExisting(filepath.Join(path, filename))
		if err != nil {
			return nil, err
		}
		owner.locks = append(owner.locks, lock)
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			return nil, errors.New("original lifecycle directory changed")
		}
	}
	return owner, nil
}
