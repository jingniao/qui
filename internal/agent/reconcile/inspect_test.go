package reconcile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/platform/vault"
)

func TestInspectCurrentAuthenticatesWithoutStartingOrRewritingState(t *testing.T) {
	root := t.TempDir()
	key := bytes.Repeat([]byte{27}, 32)
	if revision, err := InspectCurrent(root, "node-test", key); err != nil || revision != 0 {
		t.Fatal(revision, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("read-only inspection initialized state", err)
	}
	folder := filepath.Join(root, "revisions", "1")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	v, err := vault.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(state(1080, 1))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "desired.json.enc"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(Manifest{Revision: 1, Digest: digest(plain), Successful: []int64{1}})
	path := filepath.Join(root, "current.json")
	if err := os.WriteFile(path, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if revision, err := InspectCurrent(root, "node-test", key); err != nil || revision != 1 {
		t.Fatal(revision, err)
	}
	for _, bad := range []struct {
		node string
		key  []byte
	}{{"other", key}, {"node-test", bytes.Repeat([]byte{28}, 32)}, {"node-test", nil}} {
		if _, err := InspectCurrent(root, bad.node, bad.key); err == nil {
			t.Fatal("unmatched state accepted")
		}
	}
	changed := bytes.Replace(manifest, []byte(digest(plain)), []byte("bad-digest"), 1)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectCurrent(root, "node-test", key); err == nil {
		t.Fatal("wrong manifest digest accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectCurrent(root, "node-test", key); err == nil {
		t.Fatal("missing manifest treated as fresh node")
	}
}
