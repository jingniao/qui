package traffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const ConnectionScope = "proxy_user_sessions_v1"
const MaxConnectionSnapshotBytes = 1 << 20

type ConnectionAccount struct {
	CredentialID string `json:"credential_id"`
	Connections  uint64 `json:"connections"`
}
type ConnectionSnapshot struct {
	NodeID      string              `json:"node_id"`
	Revision    int64               `json:"revision"`
	CoreEpoch   string              `json:"core_epoch"`
	ObservedAt  time.Time           `json:"observed_at"`
	Scope       string              `json:"scope"`
	Connections uint64              `json:"connections"`
	Accounts    []ConnectionAccount `json:"accounts"`
}

func (s ConnectionSnapshot) Validate() error {
	if s.NodeID == "" || len(s.NodeID) > 128 || s.Revision < 1 || s.CoreEpoch == "" || len(s.CoreEpoch) > 128 || s.Scope != ConnectionScope || s.ObservedAt.IsZero() || s.Connections > 100000000 || len(s.Accounts) > 4096 {
		return errors.New("invalid live connection snapshot")
	}
	seen := map[string]bool{}
	var sum uint64
	for _, account := range s.Accounts {
		if account.CredentialID == "" || len(account.CredentialID) > 128 || seen[account.CredentialID] || account.Connections > 100000000 {
			return errors.New("invalid live connection account")
		}
		seen[account.CredentialID] = true
		sum += account.Connections
		if sum > 100000000 {
			return errors.New("live connection count exceeds bounds")
		}
	}
	if sum != s.Connections {
		return errors.New("live connection total does not match accounts")
	}
	return nil
}
func DecodeConnections(data []byte) (ConnectionSnapshot, error) {
	var s ConnectionSnapshot
	if len(data) > MaxConnectionSnapshotBytes {
		return s, errors.New("live connection snapshot too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(&struct{}{}) != io.EOF {
		return s, errors.New("invalid live connection payload")
	}
	return s, s.Validate()
}
