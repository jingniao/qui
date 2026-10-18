package xray

import (
	"context"
	"net"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

// TestRealXrayBindsConfiguredOutboundSource applies a rendered configuration
// that pins the outbound source address to this host's own IPv4 address and
// checks the source address a local listener sees. It is skipped when the host
// has no non-loopback IPv4 address.
func TestRealXrayBindsConfiguredOutboundSource(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY to run official Xray integration")
	}
	source := nonLoopbackIPv4()
	if source == "" {
		t.Skip("a local non-loopback IPv4 address is required for the outbound source test")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(source, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", source, err)
	}
	defer listener.Close()
	observed := make(chan string, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		observed <- connection.RemoteAddr().(*net.TCPAddr).IP.String()
		connection.Close()
	}()
	port := availablePort(t)
	state := testState(port, 1)
	state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{{ID: "device-credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "source-account", Secret: "source-password"}}
	state.Inbounds[0].CredentialIDs = []string{"device-credential"}
	state.Network = &desiredstate.Network{OutboundSourceIP: source}
	adapter := &Adapter{Path: binaryPath, Directory: t.TempDir(), NodeID: testNodeID}
	defer adapter.Close()
	ctx := context.Background()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	connection := socksConnect(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "source-account", "source-password", listener.Addr().(*net.TCPAddr))
	defer connection.Close()
	select {
	case seen := <-observed:
		if seen != source {
			t.Fatalf("the listener saw source %s, want %s", seen, source)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the listener saw no connection from the pinned source address")
	}
}
