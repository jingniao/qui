package agenthost

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/agentstorage"
)

func raiseFixture(t *testing.T) (string, agentstorage.Contract) {
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

// A floor that already writes the core accounting and multi-core formats needs
// neither the history nor the jobs state, so a managed start never touches them.
func TestRaiseFormatsNoopWhenFloorRaised(t *testing.T) {
	root, contract := raiseFixture(t)
	if _, err := storage.ActivateCoreAccountingFormat(root, "node", contract); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ActivateCoreInventoryFormats(root, "node", contract); err != nil {
		t.Fatal(err)
	}
	required, _, err := storage.Check(root, "node", contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := raiseFormats(context.Background(), root, "node", contract, required, nil, "", updater.Image{}, false); err != nil {
		t.Fatal(err)
	}
}

// With update jobs outstanding the proof is refused before the history is used
// and the floor stays where it was.
func TestRaiseFormatsRefusesWhileJobsPending(t *testing.T) {
	root, contract := raiseFixture(t)
	required, _, err := storage.Check(root, "node", contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := raiseFormats(context.Background(), root, "node", contract, required, nil, "", updater.Image{}, false); !errors.Is(err, ErrState) {
		t.Fatalf("pending jobs accepted: %v", err)
	}
	after, _, err := storage.Check(root, "node", contract)
	if err != nil || after != required {
		t.Fatalf("floor changed: %v", err)
	}
}
