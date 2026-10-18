package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/agentstorage"
	"testing"
)

// TestEnsureRefusesKeyInNonFreshDirectory proves Ensure never regenerates the
// configuration key for a directory that already holds persisted state. Each
// marker on its own must refuse; a genuinely fresh directory still gets a key.
func TestEnsureRefusesKeyInNonFreshDirectory(t *testing.T) {
	markers := map[string]func(t *testing.T, root string){
		"current.json": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "current.json"), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"revisions": func(t *testing.T, root string) {
			directory := filepath.Join(root, "revisions", "1")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "desired.json.enc"), []byte("sealed"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"identity": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "identity", "material.enc"), []byte("sealed"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"spool": func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "spool"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "spool", "queue.db"), []byte("db"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"spool-key": func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "spool"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "spool", "spool.key"), []byte("key"), 0600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mark := range markers {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "identity"), 0700); err != nil {
				t.Fatal(err)
			}
			mark(t, root)
			if _, err := Ensure(root, "node", agentstorage.Current("dev")); !errors.Is(err, ErrStorage) {
				t.Fatalf("Ensure did not refuse a non-fresh directory: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "configuration.key")); !os.IsNotExist(err) {
				t.Fatal("Ensure created a key in a non-fresh directory")
			}
		})
	}
	t.Run("fresh", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "identity"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := Ensure(root, "node", agentstorage.Current("dev")); err != nil {
			t.Fatalf("fresh directory was refused: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "configuration.key")); err != nil {
			t.Fatalf("fresh directory did not get a key: %v", err)
		}
	})
}

func storageFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configuration.key"), bytes.Repeat([]byte{42}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestStorageFloorOriginalKeyNodeScopeAndImmutableAdoption(t *testing.T) {
	root := storageFixture(t)
	current := agentstorage.Current("1.0.0")
	floor, err := Ensure(root, "node", current)
	if err != nil || floor != current {
		t.Fatal(floor, err)
	}
	path := filepath.Join(root, "identity", markerName)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newer := current
	newer.AgentVersion = "2.0.0"
	if floor, err := Ensure(root, "node", newer); err != nil || floor != current {
		t.Fatal("compatible version rewrote format floor", floor, err)
	}
	retained, _ := os.ReadFile(path)
	if !bytes.Equal(retained, original) {
		t.Fatal("floor replaced")
	}
	if _, _, err := Check(root, "other-node", current); err == nil {
		t.Fatal("foreign node accepted floor")
	}
	older := current
	older.Formats.ReleaseHistory.ReadMax = 2
	older.Formats.ReleaseHistory.Write = 2
	if _, _, err := Check(root, "node", older); err == nil {
		t.Fatal("legacy reader allowed unknown history")
	}
	if err := os.Remove(filepath.Join(root, "configuration.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(root, "node", current); err == nil {
		t.Fatal("missing key regenerated")
	}
	if _, err := os.Stat(filepath.Join(root, "configuration.key")); !os.IsNotExist(err) {
		t.Fatal("checker created a replacement key")
	}
}
func TestStorageFloorReadOnlyCheckAndDamageNeverReinitializes(t *testing.T) {
	root := storageFixture(t)
	current := agentstorage.Current("dev")
	entries, _ := os.ReadDir(filepath.Join(root, "identity"))
	if _, present, err := Check(root, "node", current); err != nil || present {
		t.Fatal(present, err)
	}
	after, _ := os.ReadDir(filepath.Join(root, "identity"))
	if len(after) != len(entries) {
		t.Fatal("read-only check wrote floor")
	}
	if _, err := Ensure(root, "node", current); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "identity", markerName)
	evidence := []byte("damaged floor must survive")
	if err := os.WriteFile(path, evidence, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(root, "node", current); err == nil {
		t.Fatal("damaged floor adopted as fresh")
	}
	retained, _ := os.ReadFile(path)
	if !bytes.Equal(retained, evidence) {
		t.Fatal("damage overwritten")
	}
}
func TestStorageFloorRejectsAliasedAndOversizedMarkers(t *testing.T) {
	for _, mode := range []string{"alias", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			root := storageFixture(t)
			current := agentstorage.Current("dev")
			if _, err := Ensure(root, "node", current); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "identity", markerName)
			if mode == "alias" {
				if err := os.Link(path, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, bytes.Repeat([]byte{1}, agentstorage.MaxContractBytes+1025), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := Check(root, "node", current); err == nil {
				t.Fatal("unsafe floor marker accepted")
			}
		})
	}
}
