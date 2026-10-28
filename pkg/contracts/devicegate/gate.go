// Package devicegate defines the ephemeral, fail-open device gate transport.
package devicegate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

const TTL = 3 * time.Minute

type Entry struct {
	CredentialID string   `json:"credential_id"`
	Keys         []string `json:"keys"`
}
type Request struct {
	Entries    []Entry `json:"entries"`
	TTLSeconds int     `json:"ttl_seconds"`
}

func Decode(data []byte) (Request, error) {
	var r Request
	if len(data) > 1<<20 {
		return r, errors.New("invalid device gate")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(&struct{}{}) != io.EOF || r.TTLSeconds < 1 || r.TTLSeconds > 180 || len(r.Entries) > 4096 {
		return Request{}, errors.New("invalid device gate")
	}
	for _, e := range r.Entries {
		if e.CredentialID == "" || len(e.CredentialID) > 128 || len(e.Keys) > 4096 {
			return Request{}, errors.New("invalid device gate")
		}
		for _, k := range e.Keys {
			canonical, ok := traffic.AddressKey(k)
			if !ok || canonical != k {
				return Request{}, errors.New("invalid device gate")
			}
		}
	}
	return r, nil
}
