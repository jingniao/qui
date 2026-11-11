// Package agenthost selects and supervises authenticated Agent installations.
package agenthost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/vault"
)

var ErrState = errors.New("Agent supervision requires matching original state")

const markerName = "agent-supervisor.enc"
const pendingPath = "identity/agent-supervision-init.pending"

func managedPaths() []string {
	return []string{"identity/" + markerName, "identity/agent-release-checkpoint.enc", "agent-release-history", "releases/agent-installed", "releases/agent-candidates", "agent-jobs", "identity/agent-jobs-checkpoint.enc"}
}

// CleanupInterrupted only removes uncommitted initialization, never completed
// managed state. Both ownership locks exclude an initializer or running worker.
func CleanupInterrupted(directory string) (cleaned bool, resultErr error) {
	if _, err := os.Lstat(filepath.Join(directory, pendingPath)); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	owner, err := lifecycle.TrySupervisor(directory)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	worker, err := lifecycle.Try(owner.Directory())
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, worker.Close()) }()
	// Recheck under ownership: initialization may have completed while waiting.
	if _, err := os.Lstat(filepath.Join(directory, pendingPath)); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	// Windows does not allow removing files from a directory while one of the
	// ownership lock handles in that directory is open. The recheck above is
	// protected by both locks; release them before deleting the interrupted state.
	_ = worker.Close()
	worker = nil
	_ = owner.Close()
	owner = nil
	return true, cleanupInitialization(directory)
}

func cleanupInitialization(directory string) error {
	var err error
	for _, relative := range managedPaths() {
		err = errors.Join(err, os.RemoveAll(filepath.Join(directory, relative)))
	}
	// Persist removals before removing the evidence that cleanup is required.
	for _, relative := range []string{"identity", "releases", "."} {
		path := filepath.Join(directory, relative)
		if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		err = errors.Join(err, syncDirectory(path))
	}
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(directory, pendingPath)); err != nil {
		return err
	}
	return syncDirectory(filepath.Join(directory, "identity"))
}

type record struct {
	Kind     string        `json:"kind"`
	NodeID   string        `json:"node_id"`
	Baseline updater.Image `json:"baseline"`
	Jobs     bool          `json:"jobs,omitempty"`
}

// Active treats even partial initialization as managed. Missing one file must
// never silently select the original standalone executable.
func Active(directory string) (bool, error) {
	for _, relative := range append(managedPaths(), pendingPath) {
		_, err := os.Lstat(filepath.Join(directory, filepath.FromSlash(relative)))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

func readOriginal(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, ErrState
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(f) != nil {
		return nil, ErrState
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrState
	}
	return data, nil
}

func readRecord(path string, key []byte, nodeID string) (record, error) {
	var r record
	sealed, err := readOriginal(path, 4096)
	if err != nil {
		return r, err
	}
	v, err := vault.FromKey(key)
	if err != nil {
		return r, err
	}
	data, err := v.Open(sealed)
	if err != nil {
		return r, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(&struct{}{}) != io.EOF || r.Kind != "agent-supervision-v1" || r.NodeID != nodeID || r.Baseline.Sequence != 0 {
		return record{}, ErrState
	}
	return r, nil
}

func createRecord(path string, key []byte, r record) error {
	v, err := vault.FromKey(key)
	if err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	sealed, err := v.Seal(data)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(sealed)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
