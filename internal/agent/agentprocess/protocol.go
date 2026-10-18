// Package agentprocess implements a local, fixed Agent process handshake.
// Neither the handshake nor the process API accepts remote commands or flags.
package agentprocess

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const MaxHandshakeBytes = 4096

var ErrHandshake = errors.New("managed Agent startup confirmation failed")
var noncePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Request struct {
	Kind                  string `json:"kind"`
	Nonce                 string `json:"nonce"`
	NodeID                string `json:"node_id"`
	Revision              int64  `json:"revision"`
	Version               string `json:"version"`
	AllowDegraded         bool   `json:"allow_degraded,omitempty"`
	AgentUpdates          bool   `json:"agent_updates,omitempty"`
	AgentRecovery         bool   `json:"agent_recovery,omitempty"`
	ControlOnly           bool   `json:"control_only,omitempty"`
	ConnectionFormats     bool   `json:"connection_formats,omitempty"`
	SuccessReceiptFormats bool   `json:"success_receipt_formats,omitempty"`
}

func (r Request) Validate() error {
	if r.ControlOnly && (!r.AllowDegraded || !r.AgentRecovery) {
		return ErrHandshake
	}
	if r.AgentRecovery && !r.AgentUpdates {
		return ErrHandshake
	}
	if r.Kind != "agent-start-v1" || !noncePattern.MatchString(r.Nonce) || r.NodeID == "" || len(r.NodeID) > 128 || r.Revision < 0 || r.Version == "" || len(r.Version) > 64 {
		return ErrHandshake
	}
	return nil
}

type Ready struct {
	Kind     string `json:"kind"`
	Nonce    string `json:"nonce"`
	NodeID   string `json:"node_id"`
	Revision int64  `json:"revision"`
	Version  string `json:"version"`
}

// ReadRequest consumes a single bounded frame. The caller keeps the returned
// reader for the lifetime channel so buffered extra input cannot be ignored.
func ReadRequest(input io.Reader) (Request, io.Reader, error) {
	reader := bufio.NewReader(io.LimitReader(input, MaxHandshakeBytes+1))
	var r Request
	if readFrame(reader, &r) != nil || r.Validate() != nil {
		return Request{}, nil, ErrHandshake
	}
	return r, reader, nil
}

func readFrame(reader *bufio.Reader, target any) error {
	data, err := reader.ReadBytes('\n')
	if err != nil || len(data) > MaxHandshakeBytes {
		return ErrHandshake
	}
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return ErrHandshake
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return ErrHandshake
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return ErrHandshake
		}
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrHandshake
	}
	return nil
}

func Confirm(r Request, node string, revision int64, version string, output io.Writer) error {
	return ConfirmHealth(r, node, revision, version, true, output)
}

func ConfirmHealth(r Request, node string, revision int64, version string, healthy bool, output io.Writer) error {
	if r.ControlOnly && healthy {
		return ErrHandshake
	}
	if r.Validate() != nil || node != r.NodeID || revision != r.Revision || version != r.Version {
		return ErrHandshake
	}
	kind := "agent-ready-v1"
	if !healthy {
		if !r.AllowDegraded {
			return ErrHandshake
		}
		kind = "agent-degraded-v1"
	}
	return json.NewEncoder(output).Encode(Ready{Kind: kind, Nonce: r.Nonce, NodeID: node, Revision: revision, Version: version})
}
