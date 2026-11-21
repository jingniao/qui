package xray

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/pkg/contracts/desiredstate"
	"slices"
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

// Only the ordinary path can fetch this loopback origin. A connection from a
// non-loopback source either fails to dial or receives HTTP 503.
func TestRealXrayInboundEgressProbesDriveSelection(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY to run official Xray integration")
	}
	source := nonLoopbackIPv4()
	if source == "" {
		t.Skip("requires a local non-loopback IPv4 address")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	ctx := context.Background()
	state := testState(availablePort(t), 1)
	state.OutboundGroups = []desiredstate.OutboundGroup{{
		ID: "pool", Strategy: "primary", Failover: "Block", ProbeURL: origin.URL, ProbeInterval: 30,
		Members: []desiredstate.GroupMember{{OutboundID: "direct", Weight: 1}},
	}}
	state.Inbounds[0].DefaultOutbound = "group:pool"
	bound := state.Inbounds[0]
	bound.ID, bound.Port, bound.EgressSourceIP = "bound", availablePort(t), source
	state.Inbounds = append(state.Inbounds, bound)
	a := &Adapter{Path: binary, NodeID: state.NodeID, Directory: t.TempDir()}
	defer a.Close()
	manager, err := reconcile.New(t.TempDir(), state.NodeID, a)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	probeErr := a.Probe(ctx)
	raw := a.outboundHealth["direct"]
	pinned := a.outboundHealth[core.ProbeKey(source, "direct")]
	if !raw.Healthy || pinned.CheckedAt.IsZero() || pinned.Healthy {
		t.Fatalf("default probe %+v, bound probe %+v, error %v", raw, pinned, probeErr)
	}
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatalf("probe results did not change the running routes: changed=%t, err=%v", changed, err)
	}
	var doc configDocument
	if err := json.Unmarshal(a.current.Data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, in := range state.Inbounds {
		target := ""
		for _, rule := range doc.Routing.Rules {
			if slices.Contains(rule.InboundTag, in.ID) {
				target = rule.OutboundTag
				break
			}
		}
		want := "direct"
		if in.ID == bound.ID {
			want = "block"
		}
		if target != want {
			t.Errorf("inbound %s after probes: %q, want %q", in.ID, target, want)
		}
	}
}
