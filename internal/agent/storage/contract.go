// Package storage guards the Agent's persisted format floor. Checking never
// creates keys or rebuilds data; adoption uses the original configuration key.
package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/agentstorage"
)

const markerName = "agent-storage-contract.enc"

var ErrStorage = errors.New("agent storage compatibility requires original authenticated state")

type floor struct {
	Kind     string                `json:"kind"`
	NodeID   string                `json:"node_id"`
	Contract agentstorage.Contract `json:"contract"`
}

func pinnedFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, ErrStorage
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(file) != nil {
		return nil, ErrStorage
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrStorage
	}
	return data, nil
}
func originalVault(directory string) (*vault.Vault, error) {
	key, err := pinnedFile(filepath.Join(directory, "configuration.key"), 32)
	if err != nil {
		return nil, ErrStorage
	}
	defer clear(key)
	v, err := vault.FromKey(key)
	if err != nil {
		return nil, ErrStorage
	}
	return v, nil
}

// Check is safe before opening Reconciler, Spool or release journals. A missing
// floor is an explicitly unadopted old node; a malformed floor is not absence.
func Check(directory, nodeID string, reader agentstorage.Contract) (agentstorage.Contract, bool, error) {
	if nodeID == "" || len(nodeID) > 128 || reader.Validate() != nil {
		return agentstorage.Contract{}, false, ErrStorage
	}
	data, err := pinnedFile(filepath.Join(directory, "identity", markerName), agentstorage.MaxContractBytes+1024)
	if errors.Is(err, os.ErrNotExist) {
		return reader, false, nil
	}
	if err != nil {
		return agentstorage.Contract{}, false, ErrStorage
	}
	v, err := originalVault(directory)
	if err != nil {
		return agentstorage.Contract{}, false, err
	}
	plain, err := v.Open(data)
	if err != nil {
		return agentstorage.Contract{}, false, ErrStorage
	}
	var f floor
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(&struct{}{}) != io.EOF || f.Kind != "agent-storage-floor-v1" || f.NodeID != nodeID {
		return agentstorage.Contract{}, false, ErrStorage
	}
	if f.Contract.Formats.Identity.Write > reader.Formats.Identity.ReadMax {
		return agentstorage.Contract{}, false, fmt.Errorf("%w: stored agent identity format %d is no longer supported; re-enroll this node", ErrStorage, f.Contract.Formats.Identity.Write)
	}
	if _, err := agentstorage.WithRequiredWriter(reader, f.Contract); err != nil {
		return agentstorage.Contract{}, false, ErrStorage
	}
	return f.Contract, true, nil
}

// freshDirectory reports whether directory holds no persisted agent state that a
// newly generated configuration key could orphan. A key may only be created here:
// an existing node whose key was lost must be re-enrolled, never silently
// re-keyed. Any of current.json, a non-empty revisions/ directory, an
// identity/*.enc file or either spool file marks the directory as in use.
func freshDirectory(directory string) (bool, error) {
	if _, err := os.Lstat(filepath.Join(directory, "current.json")); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	revisions, err := os.ReadDir(filepath.Join(directory, "revisions"))
	if err == nil {
		if len(revisions) > 0 {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	identity, err := os.ReadDir(filepath.Join(directory, "identity"))
	if err == nil {
		for _, entry := range identity {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".enc") {
				return false, nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	// The spool has its own key, so its queue database can be absent while the
	// store is still the node's own. Either file marks an existing store.
	for _, name := range []string{"queue.db", "spool.key"} {
		if _, err := os.Lstat(filepath.Join(directory, "spool", name)); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

func Ensure(directory, nodeID string, current agentstorage.Contract) (agentstorage.Contract, error) {
	required, present, err := Check(directory, nodeID, current)
	if err != nil || present {
		return required, err
	}
	// A brand-new node has not created its configuration key yet: the first
	// normal start does so through the reconciler. Adoption seals the floor with
	// that same original key, so create it here only when this is a genuinely
	// fresh directory. An existing marker whose key is missing is rejected by
	// Check above and is never regenerated.
	keyPath := filepath.Join(directory, "configuration.key")
	if _, statErr := os.Lstat(keyPath); errors.Is(statErr, os.ErrNotExist) {
		fresh, err := freshDirectory(directory)
		if err != nil {
			return agentstorage.Contract{}, err
		}
		if !fresh {
			return agentstorage.Contract{}, fmt.Errorf("%w: configuration key is missing", ErrStorage)
		}
		if _, err := vault.Open(keyPath); err != nil {
			return agentstorage.Contract{}, err
		}
	}
	v, err := originalVault(directory)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	plain, err := json.Marshal(floor{Kind: "agent-storage-floor-v1", NodeID: nodeID, Contract: current})
	if err != nil {
		return agentstorage.Contract{}, err
	}
	data, err := v.Seal(plain)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	folder := filepath.Join(directory, "identity")
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return agentstorage.Contract{}, ErrStorage
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	defer root.Close()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return agentstorage.Contract{}, err
	}
	temporary := ".storage-contract-" + hex.EncodeToString(random)
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	defer func() { file.Close(); root.Remove(temporary) }()
	if _, err := file.Write(data); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := file.Sync(); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := file.Close(); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := root.Link(temporary, markerName); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := root.Remove(temporary); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := syncDirectory(root); err != nil {
		return agentstorage.Contract{}, err
	}
	required, present, err = Check(directory, nodeID, current)
	if err != nil || !present {
		return agentstorage.Contract{}, ErrStorage
	}
	return required, nil
}

// Called under Worker/reconciliation ownership after native validation and
// before saving a connection-aware snapshot or binding its protected ledger.
// This is one-way activation, not permission to downgrade a stored format.
func ActivateConnectionFormats(directory, nodeID string, reader agentstorage.Contract) (agentstorage.Contract, error) {
	return activateStorageFormats(directory, nodeID, reader, 0, 0)
}
func ActivateSuccessReceiptFormat(directory, nodeID string, reader agentstorage.Contract) (agentstorage.Contract, error) {
	return activateStorageFormats(directory, nodeID, reader, 2, 0)
}

// ActivateCoreAccountingFormat is one-way activation of the durable core-run
// lifetime marker ABI. Before the floor accepts the new writer, the candidate
// image must be able to read what the new process writes; otherwise the raise is
// refused and no marker is written over an older, unreadable format.
func ActivateCoreAccountingFormat(directory, nodeID string, reader agentstorage.Contract) (agentstorage.Contract, error) {
	return activateStorageFormats(directory, nodeID, reader, 0, agentstorage.CoreAccountingFormat)
}
func activateStorageFormats(directory, nodeID string, reader agentstorage.Contract, configuration, coreAccounting int) (agentstorage.Contract, error) {
	connections := configuration == 0 && coreAccounting == 0
	required, present, err := Check(directory, nodeID, reader)
	if err != nil || !present || (connections && !agentstorage.SupportsConnectionFormats(reader)) || (configuration > 0 && reader.Formats.Configuration.ReadMax < configuration) || (coreAccounting > 0 && reader.Formats.CoreAccounting.ReadMax < coreAccounting) {
		return agentstorage.Contract{}, ErrStorage
	}
	if connections && required.Formats.DesiredState.Write >= 2 && required.Formats.Spool.Write >= 5 || configuration > 0 && required.Formats.Configuration.Write >= configuration || coreAccounting > 0 && required.Formats.CoreAccounting.Write >= coreAccounting {
		return required, nil
	}
	next, err := agentstorage.WithRequiredWriter(reader, required)
	if err != nil {
		return agentstorage.Contract{}, ErrStorage
	}
	if configuration > 0 {
		next.Formats.Configuration.Write = configuration
	} else if coreAccounting > 0 {
		next.Formats.CoreAccounting.Write = coreAccounting
	} else {
		next.Formats.DesiredState.Write = max(next.Formats.DesiredState.Write, 2)
		next.Formats.Spool.Write = max(next.Formats.Spool.Write, 5)
	}
	if next.Validate() != nil {
		return agentstorage.Contract{}, ErrStorage
	}
	original, err := pinnedFile(filepath.Join(directory, "identity", markerName), agentstorage.MaxContractBytes+1024)
	if err != nil {
		return agentstorage.Contract{}, ErrStorage
	}
	v, err := originalVault(directory)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	plain, err := json.Marshal(floor{Kind: "agent-storage-floor-v1", NodeID: nodeID, Contract: next})
	if err != nil {
		return agentstorage.Contract{}, err
	}
	sealed, err := v.Seal(plain)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	folder := filepath.Join(directory, "identity")
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return agentstorage.Contract{}, ErrStorage
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	defer root.Close()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return agentstorage.Contract{}, err
	}
	temporary := ".storage-activation-" + hex.EncodeToString(random)
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return agentstorage.Contract{}, err
	}
	defer func() { file.Close(); root.Remove(temporary) }()
	if _, err := file.Write(sealed); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := file.Sync(); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := file.Close(); err != nil {
		return agentstorage.Contract{}, err
	}
	current, err := pinnedFile(filepath.Join(directory, "identity", markerName), agentstorage.MaxContractBytes+1024)
	if err != nil || !bytes.Equal(current, original) {
		return agentstorage.Contract{}, ErrStorage
	}
	if err := root.Rename(temporary, markerName); err != nil {
		return agentstorage.Contract{}, err
	}
	if err := syncDirectory(root); err != nil {
		return agentstorage.Contract{}, err
	}
	result, present, err := Check(directory, nodeID, reader)
	if err != nil || !present || result != next {
		return agentstorage.Contract{}, ErrStorage
	}
	return result, nil
}
