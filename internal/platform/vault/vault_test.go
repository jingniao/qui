package vault

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestConfigurationEncryptionPersistsAndDetectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encryption.key")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("credential=private-test-secret")
	sealed, err := first.Seal(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatal("secret persisted in plaintext")
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := second.Open(sealed)
	if err != nil || !bytes.Equal(restored, plaintext) {
		t.Fatal("key persistence failed", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := second.Open(sealed); err == nil {
		t.Fatal("tampering accepted")
	}
	if _, err := second.Open([]byte("invalid")); err == nil {
		t.Fatal("invalid encrypted data accepted")
	}
}
