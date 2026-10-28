package traffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"time"
)

const ConnectionScope = "proxy_user_sessions_v1"
const MaxConnectionSnapshotBytes = 1 << 20

type ConnectionAccount struct {
	CredentialID      string   `json:"credential_id"`
	Connections       uint64   `json:"connections"`
	AddressKeys       []string `json:"address_keys,omitempty"`
	AddressesMeasured bool     `json:"addresses_measured,omitempty"`
}
type ConnectionSnapshot struct {
	DeviceGateSupported        bool                `json:"device_gate_supported,omitempty"`
	ObservationContractVersion int                 `json:"observation_contract_version,omitempty"`
	NodeID                     string              `json:"node_id"`
	Revision                   int64               `json:"revision"`
	CoreEpoch                  string              `json:"core_epoch"`
	ObservedAt                 time.Time           `json:"observed_at"`
	Scope                      string              `json:"scope"`
	Connections                uint64              `json:"connections"`
	Accounts                   []ConnectionAccount `json:"accounts"`
}

func (s ConnectionSnapshot) Validate() error {
	if s.ObservationContractVersion != 0 && s.ObservationContractVersion != 2 || s.DeviceGateSupported && s.ObservationContractVersion != 2 {
		return errors.New("invalid observation contract")
	}
	if s.NodeID == "" || len(s.NodeID) > 128 || s.Revision < 1 || s.CoreEpoch == "" || len(s.CoreEpoch) > 128 || s.Scope != ConnectionScope || s.ObservedAt.IsZero() || s.Connections > 100000000 || len(s.Accounts) > 4096 {
		return errors.New("invalid live connection snapshot")
	}
	seen := map[string]bool{}
	var sum uint64
	for _, account := range s.Accounts {
		if account.AddressesMeasured && s.ObservationContractVersion != 2 {
			return errors.New("invalid address observation")
		}
		if len(account.AddressKeys) > 4096 || len(account.AddressKeys) > 0 && (!account.AddressesMeasured || s.ObservationContractVersion != 2) {
			return errors.New("invalid address observation")
		}
		keys := map[string]bool{}
		for _, key := range account.AddressKeys {
			canonical, ok := AddressKey(key)
			if !ok || canonical != key || keys[key] {
				return errors.New("invalid address observation")
			}
			keys[key] = true
		}
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

// AddressKey normalizes IPv4 addresses and IPv6 /64s without retaining ports.
// Errors deliberately contain no client material.
func AddressKey(value string) (string, bool) {
	if p, err := netip.ParsePrefix(value); err == nil {
		if !p.Addr().IsLoopback() && !p.Addr().IsUnspecified() && !p.Addr().IsMulticast() && p.Addr().Is6() && p.Bits() == 64 && !p.Addr().Is4In6() && p.Addr().Zone() == "" {
			return p.Masked().String(), true
		}
		return "", false
	}
	a, err := netip.ParseAddr(value)
	if err != nil || a.Zone() != "" {
		return "", false
	}
	a = a.Unmap()
	if a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() {
		return "", false
	}
	if a.Is4() {
		return a.String(), true
	}
	return netip.PrefixFrom(a, 64).Masked().String(), true
}

// MaskAddress is shared by Owner and portal views (decision D-d).
func MaskAddress(key string) string {
	a, err := netip.ParseAddr(key)
	if err != nil {
		p, e := netip.ParsePrefix(key)
		if e != nil {
			return ""
		}
		a = p.Addr()
	}
	bits := 48
	if a.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(a, bits).Masked().String()
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
