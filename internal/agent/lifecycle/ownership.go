// Package lifecycle serializes Agent processes that share persistent state.
// The owner must outlive the core, collectors, queues and control workers.
package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"proxycontrol/internal/platform/filelock"
)

const lockName = ".agent-owner.lock"

type Ownership struct {
	directory string
	locks     []*filelock.Lock
}

func Try(directory string) (_ *Ownership, resultErr error) {
	return try(directory, lockName)
}

// TrySupervisor owns the launcher independently of its worker. Both data and
// identity aliases share these locks, while workers retain their normal locks.
func TrySupervisor(directory string) (*Ownership, error) {
	return try(directory, ".agent-supervisor.lock")
}

func try(directory, filename string) (_ *Ownership, resultErr error) {
	if directory == "" {
		return nil, errors.New("Agent data directory is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	owner := &Ownership{directory: canonical}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owner.Close())
		}
	}()
	// Lock the root before even creating an identity directory. Lock identity
	// separately so two data roots cannot operate an aliased identity at once.
	lock, err := filelock.Try(filepath.Join(canonical, filename))
	if err != nil {
		return nil, fmt.Errorf("acquire Agent ownership: %w", err)
	}
	owner.locks = append(owner.locks, lock)
	identity := filepath.Join(canonical, "identity")
	if err := os.MkdirAll(identity, 0700); err != nil {
		return nil, err
	}
	identity, err = filepath.EvalSymlinks(identity)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Stat(canonical)
	if err != nil {
		return nil, err
	}
	identityInfo, err := os.Stat(identity)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(rootInfo, identityInfo) {
		lock, err := filelock.Try(filepath.Join(identity, filename))
		if err != nil {
			return nil, fmt.Errorf("acquire Agent identity ownership: %w", err)
		}
		owner.locks = append(owner.locks, lock)
	}
	return owner, nil
}

func (o *Ownership) Directory() string { return o.directory }

func (o *Ownership) Close() error {
	if o == nil {
		return nil
	}
	var result error
	for n := len(o.locks) - 1; n >= 0; n-- {
		result = errors.Join(result, o.locks[n].Close())
	}
	o.locks = nil
	return result
}
