package reconcile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"proxycontrol/internal/agent/core"
)

// successfulApplyReceipt retains the original successful apply result so a
// replayed command returns the same answer after restart.
type successfulApplyReceipt struct {
	NodeID        string           `json:"nodeId"`
	DesiredDigest string           `json:"desiredDigest"`
	Result        core.ApplyResult `json:"result"`
}

func (m Manifest) requiresSuccessReceipt(revision int64) bool {
	for _, retained := range m.SuccessReceipts {
		if retained == revision {
			return true
		}
	}
	return false
}

func validateSuccessReceiptMarkers(manifest Manifest) error {
	if len(manifest.SuccessReceipts) > 5 {
		return errors.New("invalid successful apply receipt markers")
	}
	seen := map[int64]bool{}
	for _, revision := range manifest.SuccessReceipts {
		retained := false
		for _, success := range manifest.Successful {
			retained = retained || success == revision
		}
		if revision < 1 || seen[revision] || !retained {
			return errors.New("invalid successful apply receipt markers")
		}
		seen[revision] = true
	}
	return nil
}

func (r *Reconciler) verifyRequiredSuccessReceipt(manifest Manifest) error {
	if !manifest.requiresSuccessReceipt(manifest.Revision) {
		return nil
	}
	receipt, err := r.readSuccessReceipt(manifest.Revision, manifest.Digest)
	if err != nil {
		return err
	}
	if receipt == nil {
		return errors.New("required original successful apply receipt is missing")
	}
	return nil
}

func (r *Reconciler) successReceiptPath(revision int64) string {
	return filepath.Join(r.Directory, "revisions", strconv.FormatInt(revision, 10), "apply-success.json.enc")
}

func (r *Reconciler) readSuccessReceipt(revision int64, desiredDigest string) (*core.ApplyResult, error) {
	sealed, err := readInspectionFile(r.successReceiptPath(revision), (16<<10)+1024)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // Legacy manifests have no separately retained success.
	}
	if err != nil {
		return nil, err
	}
	plain, err := r.vault.Open(sealed)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	var receipt successfulApplyReceipt
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil || decoder.Decode(&struct{}{}) != io.EOF || receipt.NodeID != r.NodeID || receipt.DesiredDigest != desiredDigest || receipt.Result.Revision != revision || !receipt.Result.Success || receipt.Result.RolledBack || receipt.Result.Code != "success" || len(receipt.Result.Message) > 4096 {
		return nil, errors.New("original successful apply receipt mismatch")
	}
	return &receipt.Result, nil
}

func (r *Reconciler) writeSuccessReceipt(desiredDigest string, result core.ApplyResult) error {
	if result.Revision < 1 || !result.Success || result.RolledBack || result.Code != "success" || len(result.Message) > 4096 {
		return errors.New("invalid successful apply result")
	}
	stored, err := r.readSuccessReceipt(result.Revision, desiredDigest)
	if err != nil {
		return err
	}
	if stored != nil {
		return nil // Keep the original result, including its original message.
	}
	receipt := successfulApplyReceipt{NodeID: r.NodeID, DesiredDigest: desiredDigest, Result: result}
	plain, err := json.Marshal(receipt)
	if err != nil || len(plain) > 16<<10 {
		return errors.New("successful apply receipt exceeds limit")
	}
	sealed, err := r.vault.Seal(plain)
	clear(plain)
	if err != nil {
		return err
	}
	return writeFile(r.successReceiptPath(result.Revision), sealed)
}
