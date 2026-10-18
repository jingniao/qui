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
	probe, err := ProbeAgentImage(ctx, directory, image, agentstorage.Current("new-parent"))
	if err != nil || probe.Contract.AgentUpdateProtocol != 1 || probe.Contract.AgentRecoveryProtocol != 0 {
		t.Fatal("old executable probe failed or invented recovery", probe, err)
	}
	out, diagnostic, err := runAgentProbe(ctx, binary, "--storage-contract", "--storage-contract-version=2")
	if !legacyProbeFlag(err, out, diagnostic) {
		t.Fatal("fixture does not exercise unsupported flag", err, string(diagnostic))
	}
	if legacyProbeFlag(err, []byte("partial descriptor"), diagnostic) || legacyProbeFlag(err, nil, []byte("other execution failure")) {
		t.Fatal("arbitrary failed probe accepted as legacy")
	}
}
