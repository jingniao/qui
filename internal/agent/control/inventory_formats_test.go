package control

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/storage"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/desiredstate"
)

func inventoryFixture(t *testing.T) (string, agentstorage.Contract) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "identity"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configuration.key"), bytes.Repeat([]byte{42}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	contract := agentstorage.Current("1.5.1")
	if _, err := storage.Ensure(root, "node", contract); err != nil {
		t.Fatal(err)
	}
	return root, contract
}

func singBoxState() desiredstate.State {
	return desiredstate.State{Inbounds: []desiredstate.Inbound{{Core: "sing-box"}}}
}

// With the floor already raised, an explicit-core inbound is published without
// running the activation check (which would need the supervisor's history lock).
func TestExplicitCoreSkipsCheckWhenFloorRaised(t *testing.T) {
	t.Parallel()
	root, contract := inventoryFixture(t)
	if _, err := storage.ActivateCoreInventoryFormats(root, "node", contract); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := activateInventoryFormats(root, "node", contract, singBoxState(), func() error { called = true; return storage.ErrStorage }); err != nil || called {
		t.Fatalf("raised floor still checked: called=%v err=%v", called, err)
	}
}

// An unraised floor runs the check; a refusal is returned and nothing is raised,
// a pass raises the floor.
func TestExplicitCoreChecksWhenFloorNotRaised(t *testing.T) {
	t.Parallel()
	root, contract := inventoryFixture(t)
	refused := errors.New("supervisor did not raise")
	if err := activateInventoryFormats(root, "node", contract, singBoxState(), func() error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("refusal not returned: %v", err)
	}
	if floor, _, _ := storage.Check(root, "node", contract); floor.Formats.Configuration.Write >= 4 {
		t.Fatal("floor raised despite refusal")
	}
	if err := activateInventoryFormats(root, "node", contract, singBoxState(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if floor, _, _ := storage.Check(root, "node", contract); floor.Formats.Configuration.Write < 4 {
		t.Fatal("floor not raised after a passing check")
	}
}

func TestDefaultCoreNeverChecks(t *testing.T) {
	t.Parallel()
	root, contract := inventoryFixture(t)
	if err := activateInventoryFormats(root, "node", contract, desiredstate.State{Inbounds: []desiredstate.Inbound{{}}}, func() error { return errors.New("called") }); err != nil {
		t.Fatal(err)
	}
}
