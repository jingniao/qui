package updater

import (
	"context"
	"os"
	"proxycontrol/pkg/contracts/agentstorage"
	"testing"
)

func TestActualLegacyAgentProbeFallsBackOnlyForUnknownQueryFlag(t *testing.T) {
	binary := os.Getenv("LEGACY_AGENT_BINARY")
	if binary == "" {
		t.Skip("set an actual pre-extension Agent")
	}
	ctx := context.Background()
	image, err := InspectImage(ctx, "proxy-agent", binary, "1.0.0-storage-test")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if _, err := MaterializeImage(ctx, directory, binary, "proxy-agent", image); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeAgentImage(ctx, directory, image, agentstorage.Current("new-parent")); err == nil {
		t.Fatal("pre-extension executable accepted without the required storage and control-only contracts")
	}
}
