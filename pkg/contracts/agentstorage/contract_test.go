package agentstorage

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReplacementKeepsRequiredWritesAndFallbackReadability(t *testing.T) {
	current := Current("1.0.0")
	next := current
	next.AgentVersion = "1.1.0"
	if err := CheckReplacement(current, next); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"old-reader", "older-writer", "unreadable-new-writer", "missing-format", "wrong-platform", "wrong-component"} {
		t.Run(mode, func(t *testing.T) {
			candidate := next
			switch mode {
			case "old-reader":
				candidate.Formats.ReleaseHistory = Format{ReadMax: 2, Write: 2}
			case "older-writer":
				candidate.Formats.Spool = Format{ReadMax: 4, Write: 3}
			case "unreadable-new-writer":
				candidate.Formats.UpdateJournal = Format{ReadMax: 3, Write: 3}
			case "missing-format":
				candidate.Formats.ReleaseHold = Format{}
			case "wrong-platform":
				if current.OS == "windows" {
					candidate.OS = "linux"
				} else {
					candidate.OS = "windows"
				}
			case "wrong-component":
				candidate.Component = "xray"
			}
			if CheckReplacement(current, candidate) == nil {
				t.Fatal("unsafe Agent replacement accepted")
			}
		})
	}
}

func TestRecoveryCapabilityIsIndependentAndRequiresUpdateProtocol(t *testing.T) {
	current := Current("recovery")
	if current.AgentRecoveryProtocol != 1 {
		t.Fatal("recovery capability missing")
	}
	legacy := current
	legacy.AgentRecoveryProtocol = 0
	data, _ := json.Marshal(legacy)
	decoded, err := Decode(data)
	if err != nil || decoded.AgentRecoveryProtocol != 0 || CheckReplacement(legacy, current) != nil {
		t.Fatal("missing recovery capability changed storage compatibility", err)
	}
	for _, mode := range []string{"negative", "future", "no-update"} {
		candidate := current
		switch mode {
		case "negative":
			candidate.AgentRecoveryProtocol = -1
		case "future":
			candidate.AgentRecoveryProtocol = 2
		case "no-update":
			candidate.AgentUpdateProtocol = 0
		}
		if candidate.Validate() == nil {
			t.Fatal("invalid recovery capability accepted", mode)
		}
	}
}
func TestStorageContractStrictDecodeRejectsMissingUnknownDuplicateAndTrailing(t *testing.T) {
	original := Current("test")
	data, _ := json.Marshal(original)
	parsed, err := Decode(data)
	if err != nil || parsed != original {
		t.Fatal(parsed, err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, data...), []byte("{}")...), bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1), bytes.Replace(data, []byte(`"read_max":2`), []byte(`"read_max":2,"read_max":2`), 1), bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"path":"anything"`), 1), bytes.Replace(data, []byte(`"release_hold":{"read_max":2,"write":2}`), []byte(`"release_hold":{}`), 1)} {
		if _, err := Decode(bad); err == nil {
			t.Fatal("ambiguous/incomplete contract accepted")
		}
	}
}

func TestManagedCapabilityDoesNotPretendLegacyBinaryCanBeSupervised(t *testing.T) {
	current := Current("new")
	if current.ManagedProtocol != 1 {
		t.Fatal("managed protocol not declared")
	}
	legacy := current
	legacy.ManagedProtocol = 0
	legacy.AgentUpdateProtocol = 0
	legacy.AgentRecoveryProtocol = 0
	if legacy.Validate() != nil || CheckReplacement(legacy, current) != nil {
		t.Fatal("data compatibility was confused with launch capability")
	}
	for _, unsupported := range []int{-1, 2} {
		candidate := current
		candidate.ManagedProtocol = unsupported
		if candidate.Validate() == nil {
			t.Fatal("unknown managed protocol accepted")
		}
	}
}
