package reconcile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

// InspectCurrent authenticates the retained snapshot without opening a core
// or generating a key. An authoritative handoff decision requires data-directory
// ownership. A live observation may reject a request early, never authorize it.
func InspectCurrent(directory, nodeID string, key []byte) (int64, error) {
	v, err := vault.FromKey(key)
	if err != nil {
		return 0, err
	}
	manifest, err := readCurrentManifest(directory)
	if err != nil {
		return 0, err
	}
	if manifest == nil {
		return 0, nil
	}
	if manifest.Revision < 1 {
		return 0, errors.New("invalid current manifest")
	}
	sealed, err := readInspectionFile(filepath.Join(directory, "revisions", strconv.FormatInt(manifest.Revision, 10), "desired.json.enc"), (2<<20)+1024)
	if err != nil {
		return 0, err
	}
	plain, err := v.Open(sealed)
	if err != nil {
		return 0, err
	}
	defer clear(plain)
	state, err := desiredstate.Decode(plain)
	if err != nil || state.NodeID != nodeID || state.Revision != manifest.Revision || digest(plain) != manifest.Digest {
		return 0, errors.New("retained revision identity or checksum mismatch")
	}
	r := &Reconciler{Directory: directory, NodeID: nodeID, vault: v}
	if err := r.verifyRequiredSuccessReceipt(*manifest); err != nil {
		return 0, err
	}
	return state.Revision, nil
}

func readInspectionFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, errors.New("unsafe retained snapshot file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(f) != nil {
		return nil, errors.New("retained snapshot file changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, errors.New("retained snapshot exceeds limit")
	}
	return data, nil
}
