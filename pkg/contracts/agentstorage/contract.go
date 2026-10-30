// Package agentstorage describes the fixed Agent storage ABI. It carries no
// filenames, credentials, remote commands or executable arguments.
package agentstorage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"runtime"
)

const MaxContractBytes = 8192

// CoreAccountingFormat is the on-disk version of the durable core-run lifetime
// marker ABI. It advances whenever the marker content would be unreadable or
// misread by an earlier reader: version 4 carries the versioned marker with its
// host/boot/namespace environment binding. A reader that cannot read it must be
// refused before the newer marker is written, never silently downgraded.
const CoreAccountingFormat = 4

var ErrContract = errors.New("agent storage contract is invalid or incompatible")
var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+\-]{0,63}$`)

type Format struct {
	ReadMax int `json:"read_max"`
	Write   int `json:"write"`
}
type Formats struct {
	Configuration  Format `json:"configuration"`
	Identity       Format `json:"identity"`
	DesiredState   Format `json:"desired_state"`
	Spool          Format `json:"spool"`
	CoreAccounting Format `json:"core_accounting"`
	ReleaseHistory Format `json:"release_history"`
	UpdateJournal  Format `json:"update_journal"`
	ReleaseHold    Format `json:"release_hold"`
}
type Contract struct {
	SchemaVersion         int     `json:"schema_version"`
	Component             string  `json:"component"`
	AgentVersion          string  `json:"agent_version"`
	OS                    string  `json:"os"`
	Architecture          string  `json:"architecture"`
	Formats               Formats `json:"formats"`
	ManagedProtocol       int     `json:"managed_protocol,omitempty"`
	AgentUpdateProtocol   int     `json:"agent_update_protocol,omitempty"`
	AgentRecoveryProtocol int     `json:"agent_recovery_protocol,omitempty"`
}

func version(n int) Format { return Format{ReadMax: n, Write: n} }
func Current(agentVersion string) Contract {
	return Contract{SchemaVersion: 1, Component: "proxy-agent", AgentVersion: agentVersion, OS: runtime.GOOS, Architecture: runtime.GOARCH, ManagedProtocol: 1, AgentUpdateProtocol: 1, AgentRecoveryProtocol: 1, Formats: Formats{Configuration: Format{ReadMax: 4, Write: 1}, Identity: Format{ReadMax: 2, Write: 2}, DesiredState: Format{ReadMax: 3, Write: 1}, Spool: Format{ReadMax: 6, Write: 4}, CoreAccounting: Format{ReadMax: CoreAccountingFormat, Write: 2}, ReleaseHistory: version(3), UpdateJournal: version(2), ReleaseHold: Format{ReadMax: 3, Write: 2}}}
}

// Optional formats preserve legacy writes until connection protection or
// atomic certificate/key material is explicitly activated.
func WithRequiredWriter(reader, required Contract) (Contract, error) {
	if reader.Validate() != nil || required.Validate() != nil || reader.OS != required.OS || reader.Architecture != required.Architecture {
		return Contract{}, ErrContract
	}
	if reader.Formats.DesiredState.Write < required.Formats.DesiredState.Write && reader.Formats.DesiredState.ReadMax >= 2 {
		reader.Formats.DesiredState.Write = required.Formats.DesiredState.Write
	}
	if reader.Formats.Configuration.Write < required.Formats.Configuration.Write && reader.Formats.Configuration.ReadMax >= required.Formats.Configuration.Write {
		reader.Formats.Configuration.Write = required.Formats.Configuration.Write
	}
	if reader.Formats.Spool.Write < required.Formats.Spool.Write && reader.Formats.Spool.ReadMax >= 5 {
		reader.Formats.Spool.Write = required.Formats.Spool.Write
	}
	if reader.Formats.CoreAccounting.Write < required.Formats.CoreAccounting.Write && reader.Formats.CoreAccounting.ReadMax >= required.Formats.CoreAccounting.Write {
		reader.Formats.CoreAccounting.Write = required.Formats.CoreAccounting.Write
	}
	if reader.Formats.ReleaseHold.Write < required.Formats.ReleaseHold.Write && reader.Formats.ReleaseHold.ReadMax >= required.Formats.ReleaseHold.Write {
		reader.Formats.ReleaseHold.Write = required.Formats.ReleaseHold.Write
	}
	if reader.Validate() != nil || CheckReplacement(required, reader) != nil {
		return Contract{}, ErrContract
	}
	return reader, nil
}

func SupportsConnectionFormats(c Contract) bool {
	return c.Validate() == nil && c.Formats.DesiredState.ReadMax >= 2 && c.Formats.Spool.ReadMax >= 5
}
func (f Formats) all() [8]Format {
	return [8]Format{f.Configuration, f.Identity, f.DesiredState, f.Spool, f.CoreAccounting, f.ReleaseHistory, f.UpdateJournal, f.ReleaseHold}
}
func (c Contract) Validate() error {
	if c.AgentRecoveryProtocol < 0 || c.AgentRecoveryProtocol > 1 || c.AgentRecoveryProtocol > 0 && c.AgentUpdateProtocol != 1 {
		return ErrContract
	}
	if c.ManagedProtocol < 0 || c.ManagedProtocol > 1 {
		return ErrContract
	}
	if c.AgentUpdateProtocol < 0 || c.AgentUpdateProtocol > 1 || c.AgentUpdateProtocol > 0 && c.ManagedProtocol != 1 {
		return ErrContract
	}
	if c.SchemaVersion != 1 || c.Component != "proxy-agent" || !versionPattern.MatchString(c.AgentVersion) || c.OS != "linux" && c.OS != "windows" || c.Architecture != "amd64" && c.Architecture != "arm64" {
		return ErrContract
	}
	for _, f := range c.Formats.all() {
		if f.Write < 1 || f.ReadMax < f.Write || f.ReadMax > 32 {
			return ErrContract
		}
	}
	return nil
}

// CheckReplacement proves declared mutual readability: the candidate can read
// all current writes and cannot introduce writes unreadable by the fallback.
// A candidate also cannot downgrade the semantics of an already written ABI.
func CheckReplacement(current, candidate Contract) error {
	if current.Validate() != nil || candidate.Validate() != nil || current.OS != candidate.OS || current.Architecture != candidate.Architecture {
		return ErrContract
	}
	old, next := current.Formats.all(), candidate.Formats.all()
	for n, f := range next {
		if f.ReadMax < old[n].Write || f.Write < old[n].Write || f.Write > old[n].ReadMax {
			return ErrContract
		}
	}
	return nil
}
func Decode(data []byte) (Contract, error) {
	var c Contract
	if len(data) == 0 || len(data) > MaxContractBytes || uniqueKeys(data) != nil {
		return c, ErrContract
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(&struct{}{}) != io.EOF {
		return c, ErrContract
	}
	return c, c.Validate()
}
func uniqueKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if depth > 4 {
			return ErrContract
		}
		token, err := d.Token()
		if err != nil {
			return ErrContract
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if delimiter != '{' {
			return ErrContract
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return ErrContract
			}
			seen[name] = true
			if err := value(depth + 1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrContract
		}
		return nil
	}
	if value(0) != nil {
		return ErrContract
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrContract
	}
	return nil
}
