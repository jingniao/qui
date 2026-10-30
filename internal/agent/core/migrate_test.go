package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyRuntimeMigrationResumesAndRefusesConflicts(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"accounting.key", "core-run.json.enc", "access.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := MigratePrimaryRuntime(dir, "xray"); err != nil {
		t.Fatal(err)
	}
	if err := MigratePrimaryRuntime(dir, "xray"); err != nil {
		t.Fatal("migration not restartable", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "xray", "accounting.key"))
	if err != nil || string(data) != "accounting.key" {
		t.Fatal(string(data), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "accounting.key"), []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigratePrimaryRuntime(dir, "xray"); err == nil {
		t.Fatal("migration overwrote a retained key")
	}
}
