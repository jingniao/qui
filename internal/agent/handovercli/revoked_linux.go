//go:build linux

package handovercli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/vault"
)

// This encrypted request evidence is never read as a live authorization. It
// allows the barrier to close the original receipt after revocation or a reboot.
func revokedPath(s *localState, requestID string) string {
	return filepath.Join(s.config.DataDirectory, "handover-cli-request-"+requestID+".json.enc")
}

func saveRevoked(s *localState, r core.HandoverRequest) (resultErr error) {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	sealed, err := s.key.Seal(data)
	clear(data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.config.DataDirectory, ".handover-revoked-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	_, writeErr := f.Write(sealed)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), revokedPath(s, r.ID)); err != nil {
		return err
	}
	return vault.SyncDir(s.config.DataDirectory)
}

func loadRevoked(s *localState, requestID string) (_ *core.HandoverRequest, resultErr error) {
	if !validDigest(requestID) {
		return nil, core.ErrHandoverInvalid
	}
	info, err := os.Lstat(revokedPath(s, requestID))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > core.MaxHandoverMaterialBytes {
		return nil, core.ErrHandoverInvalid
	}
	f, err := os.Open(revokedPath(s, requestID))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, core.ErrHandoverConflict
	}
	if err := filelock.CheckSingleLink(f); err != nil {
		return nil, err
	}
	sealed, err := io.ReadAll(io.LimitReader(f, core.MaxHandoverMaterialBytes+1))
	if err != nil {
		return nil, err
	}
	if len(sealed) > core.MaxHandoverMaterialBytes {
		return nil, core.ErrHandoverInvalid
	}
	data, err := s.key.Open(sealed)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	var r core.HandoverRequest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return nil, err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return nil, core.ErrHandoverInvalid
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if r.NodeID != s.nodeID || r.ID != requestID {
		return nil, core.ErrHandoverConflict
	}
	return &r, nil
}
