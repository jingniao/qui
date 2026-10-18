package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAgentCommandsStaySeparateFromCoreCommandsAndBindResults(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	signed, err := Sign(Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "test", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 1, Bytes: 100, SHA256: strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, key)
	if err != nil {
		t.Fatal(err)
	}
	r := AgentUpdateRequest{SchemaVersion: 1, ID: "agent-one", NodeID: "node", Revision: 1, Operation: "install", Release: signed}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := DecodeAgentUpdateRequest(data); err != nil || parsed != r {
		t.Fatal(parsed, err)
	}
	if _, err := DecodeUpdateRequest(data); err == nil {
		t.Fatal("old core decoder accepted Agent executable")
	}
	core := r
	core.Release.Manifest.Component = "xray"
	coreData, _ := json.Marshal(core)
	if _, err := DecodeAgentUpdateRequest(coreData); err == nil {
		t.Fatal("Agent decoder accepted core command")
	}
	result := UpdateResult{ID: r.ID, NodeID: r.NodeID, Revision: r.Revision, TargetDigest: r.Release.Manifest.SHA256, Status: "success", Code: "success", InstalledDigest: r.Release.Manifest.SHA256, InstalledVersion: r.Release.Manifest.Version, InstalledSequence: 1}
	if err := ValidateAgentResult(r, result); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "node", "revision", "digest", "version", "sequence"} {
		bad := result
		switch field {
		case "id":
			bad.ID = "other"
		case "node":
			bad.NodeID = "other"
		case "revision":
			bad.Revision++
		case "digest":
			bad.TargetDigest = strings.Repeat("b", 64)
		case "version":
			bad.InstalledVersion = "other"
		case "sequence":
			bad.InstalledSequence++
		}
		if ValidateAgentResult(r, bad) == nil {
			t.Fatal("unbound result accepted", field)
		}
	}
}
