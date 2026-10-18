package identity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRejectsMissingAndCorruptIdentityWithoutRecreating(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "identity.json")
	if state, err := Load(directory); state != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing identity was accepted", state, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read recreated missing identity", err)
	}
	for _, contents := range []string{`{"node_id":`, `{"node_id":"","last_acked":42}`} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if state, err := Load(directory); state != nil || err == nil {
			t.Fatal("corrupt identity was accepted", state, err)
		}
		retained, err := os.ReadFile(path)
		if err != nil || string(retained) != contents {
			t.Fatal("read replaced corrupt identity", err)
		}
	}
}
