package singbox

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/testutil"
	"strconv"
	"testing"
)

// The Agent starts sing-box with multipathtcp=0 (core.CoreEnvironment), so its
// inbounds listen with plain TCP; CVE-2026-98432's root exploit needs an MPTCP
// listener. The control runs the same configuration with Go's own default.
func TestRealSingboxListenersArePlainTCP(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for the MPTCP listener check")
	}
	testutil.RequireKernelMPTCP(t)
	ctx := context.Background()
	state := stateFixture(t, "http", freePort(t))
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
	defer adapter.Close()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, rendered.Data, 0o600); err != nil {
		t.Fatal(err)
	}
	port := state.Inbounds[0].Port
	byDefault := testutil.CoreDefaultsToMPTCP(t, binary, []string{"run", "--disable-color", "-c", config}, port)
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if testutil.MPTCPDial(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) {
		t.Fatal("sing-box started by the Agent listens with MPTCP (CVE-2026-98432 exposure)")
	}
	t.Logf("MPTCP: sing-box with Go's default listens with MPTCP: %v; started by the Agent: plain TCP", byDefault)
}
