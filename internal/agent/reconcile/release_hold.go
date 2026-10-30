package reconcile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/filelock"
)

const releaseHoldName = "core-update-hold.enc"

type releaseHold struct {
	Kind     string `json:"kind"`
	NodeID   string `json:"node_id"`
	Revision int64  `json:"revision"`
}

func (r *Reconciler) loadReleaseHold() {
	path := filepath.Join(r.Directory, releaseHoldName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		r.coreUpdateHold = true
		r.coreUpdateHoldInvalid = true
		return
	}
	file, err := os.Open(path)
	// An unreadable, partial or unauthenticated marker also remains closed.
	r.coreUpdateHold = true
	if err != nil {
		r.coreUpdateHoldInvalid = true
		return
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(file) != nil {
		r.coreUpdateHoldInvalid = true
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		r.coreUpdateHoldInvalid = true
		return
	}
	plain, err := r.vault.Open(data)
	if err != nil {
		r.coreUpdateHoldInvalid = true
		return
	}
	var marker releaseHold
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if d.Decode(&marker) != nil || d.Decode(&struct{}{}) != io.EOF || marker.Kind != "core-release-hold-v1" || marker.NodeID != r.NodeID || marker.Revision != r.manifest.Revision || marker.Revision < 1 {
		r.coreUpdateHoldInvalid = true
	}
}
func (r *Reconciler) holdCoreUpdate() error {
	r.coreUpdateHold = true
	if r.coreUpdateHoldInvalid {
		return core.ErrReleaseRecovery
	}
	data, err := json.Marshal(releaseHold{Kind: "core-release-hold-v1", NodeID: r.NodeID, Revision: r.manifest.Revision})
	if err != nil {
		return err
	}
	sealed, err := r.vault.Seal(data)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(r.Directory, releaseHoldName), sealed); err != nil {
		return err
	}
	return syncReleaseHoldDirectory(r.Directory)
}
func (r *Reconciler) releaseCoreUpdate() error {
	if r.coreUpdateHoldInvalid {
		return core.ErrReleaseRecovery
	}
	if err := os.Remove(filepath.Join(r.Directory, releaseHoldName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncReleaseHoldDirectory(r.Directory); err != nil {
		return err
	}
	r.coreUpdateHold = false
	return nil
}
func (r *Reconciler) CoreUpdateHeld() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.anyCoreHold() || r.controlOnly
}
