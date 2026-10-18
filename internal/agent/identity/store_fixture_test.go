package identity

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/agentstorage"
)

// The fixture is an agent store written by the code at 0c619ba in the current
// (Write=2) identity format. It carries only test-generated keys.
const (
	fixtureStoreDir        = "testdata/store_write2"
	fixtureStoreNodeID     = "fixture-node-0001"
	fixtureCertFingerprint = "828861c54797c37857c29394f9b987c78334cdc3fdd0cd0f9c3359b4aa89d6fb"
)

func copyStore(t *testing.T, source string) string {
	t.Helper()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"configuration.key"} {
		copyStoreFile(t, filepath.Join(source, name), filepath.Join(target, name))
	}
	for _, name := range []string{"client.key", "client.crt", "node-ca.crt", "identity.json", "agent-storage-contract.enc"} {
		copyStoreFile(t, filepath.Join(source, "identity", name), filepath.Join(target, "identity", name))
	}
	return target
}

func copyStoreFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// TestWrite2StoreFixtureOpensUnchanged proves the current code opens a store
// written by the pre-refactor code in the Write=2 format and reads the same
// node ID and certificate.
func TestWrite2StoreFixtureOpensUnchanged(t *testing.T) {
	directory := filepath.Join(fixtureStoreDir, "identity")
	state, err := Load(directory)
	if err != nil {
		t.Fatalf("open fixture identity: %v", err)
	}
	if state.NodeID != fixtureStoreNodeID {
		t.Fatalf("fixture node ID changed: %q", state.NodeID)
	}
	pair, err := LoadKeyPair(directory)
	if err != nil {
		t.Fatalf("load fixture key pair: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if id, err := pki.NodeID(leaf); err != nil || id != fixtureStoreNodeID {
		t.Fatalf("fixture certificate node ID changed: %q %v", id, err)
	}
	if fingerprint := pki.Fingerprint(leaf); fingerprint != fixtureCertFingerprint {
		t.Fatalf("fixture certificate fingerprint changed: %s", fingerprint)
	}
	fixtureCertificate, err := os.ReadFile(filepath.Join(directory, "client.crt"))
	if err != nil {
		t.Fatal(err)
	}
	materialCertificate, key, err := state.Material()
	defer clear(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fixtureCertificate, materialCertificate) {
		t.Fatal("fixture certificate bytes changed")
	}
	required, present, err := storage.Check(fixtureStoreDir, state.NodeID, agentstorage.Current("test"))
	if err != nil || !present {
		t.Fatalf("open fixture storage floor: %v present=%v", err, present)
	}
	if required.Formats.Identity.Write != 2 || required.Formats.Identity.ReadMax != 2 {
		t.Fatalf("fixture identity format changed: %+v", required.Formats.Identity)
	}
}

// TestDeletedIdentityFormatStoreIsRefusedWithReenroll proves a store whose
// identity floor uses a removed format is refused with a re-enroll error rather
// than crashing or silently resetting.
func TestDeletedIdentityFormatStoreIsRefusedWithReenroll(t *testing.T) {
	root := copyStore(t, fixtureStoreDir)
	key, err := os.ReadFile(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	v, err := vault.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	contract := agentstorage.Current("1.0.0")
	contract.Formats.Identity = agentstorage.Format{ReadMax: 5, Write: 5}
	plain, err := json.Marshal(struct {
		Kind     string                `json:"kind"`
		NodeID   string                `json:"node_id"`
		Contract agentstorage.Contract `json:"contract"`
	}{"agent-storage-floor-v1", fixtureStoreNodeID, contract})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "identity", "agent-storage-contract.enc"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = storage.Check(root, fixtureStoreNodeID, agentstorage.Current("test"))
	if err == nil {
		t.Fatal("removed identity format was accepted")
	}
	if !errors.Is(err, storage.ErrStorage) || !strings.Contains(err.Error(), "re-enroll") {
		t.Fatalf("removed identity format did not report re-enroll: %v", err)
	}
}
