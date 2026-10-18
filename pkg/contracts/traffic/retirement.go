package traffic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
)

// BudgetRetirement is a fixed, version-bound request to seal an already paused
// allowance. A local seal alone does not release any central reservation.
type BudgetRetirement struct {
	ID             string `json:"id"`
	NodeID         string `json:"node_id"`
	UserID         string `json:"user_id"`
	BudgetID       string `json:"budget_id"`
	Sequence       int64  `json:"sequence"`
	CeilingBytes   int64  `json:"ceiling_bytes"`
	SpentBytes     int64  `json:"spent_bytes"`
	PauseRevision  int64  `json:"pause_revision"`
	StoreID        string `json:"store_id"`
	SourceSequence int64  `json:"source_sequence"`
	CoreEpoch      string `json:"core_epoch"`
	SourceDigest   string `json:"source_digest"`
}
type BudgetRetirementReceipt struct {
	Request      BudgetRetirement `json:"request"`
	ClosedDigest string           `json:"closed_digest"`
}

const MaxBudgetRetirementBytes = 16 << 10

// validDigest accepts a lowercase hex SHA-256. It lived with the runtime-source
// contract, which the agent no longer speaks.
func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && value == hex.EncodeToString(data)
}

func (r BudgetRetirement) Validate() error {
	if !validID(r.ID) || !validID(r.NodeID) || !validID(r.UserID) || !validID(r.BudgetID) || !validID(r.StoreID) || !validID(r.CoreEpoch) || r.Sequence < 1 || r.Sequence == math.MaxInt64 || r.CeilingBytes < 0 || r.SpentBytes < 0 || r.SpentBytes > r.CeilingBytes || r.PauseRevision < 1 || r.SourceSequence < 1 || !validDigest(r.SourceDigest) {
		return errors.New("invalid budget retirement scope")
	}
	return nil
}
func (r BudgetRetirement) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (r BudgetRetirementReceipt) Validate() error {
	if err := r.Request.Validate(); err != nil {
		return err
	}
	if !validDigest(r.ClosedDigest) {
		return errors.New("invalid budget retirement closure")
	}
	return nil
}
func DecodeBudgetRetirement(data []byte) (BudgetRetirement, error) {
	var value BudgetRetirement
	err := decodeRetirementObject(data, &value)
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}
func DecodeBudgetRetirementReceipt(data []byte) (BudgetRetirementReceipt, error) {
	var value BudgetRetirementReceipt
	err := decodeRetirementObject(data, &value)
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}
func decodeRetirementObject(data []byte, target any) error {
	if len(data) < 2 || len(data) > MaxBudgetRetirementBytes {
		return errors.New("budget retirement exceeds bounds")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid budget retirement JSON")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("extra budget retirement data")
	}
	return nil
}
