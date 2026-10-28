package singbox

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/access"
	"strconv"
	"testing"
	"time"
)

func TestRealDeviceGateSingboxClosesSourceWithinThreeSeconds(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("awaiting CI/Owner run: SINGBOX_BINARY")
	}
	ctx := context.Background()
	source := testutil.GateSource(t)
	target := testutil.GateEcho(t)
	s := stateFixture(t, "http", freePort(t))
	a := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: s.NodeID}
	defer a.Close()
	a.ConfigureRuntime(core.RuntimeHooks{EnableStats: true, AccessSink: func(access.Rollup) error { return nil }})
	rendered, err := a.Render(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := a.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Inbounds[0].Port))
	credential := s.Credentials[0]
	c, err := testutil.GateTunnel(proxy, target, credential.Username, credential.Secret, source)
	if err != nil {
		t.Fatal("tunnel failed")
	}
	defer c.Close()
	if !testutil.GateRoundTrip(c) {
		t.Fatal("baseline traffic failed")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		sample, err := a.ConnectionSnapshot(ctx)
		if err == nil && len(sample.Accounts) > 0 && len(sample.Accounts[0].AddressKeys) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source/user join unavailable")
		}
		time.Sleep(20 * time.Millisecond)
	}
	command, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []map[string]any{{"credential_id": credential.ID, "keys": []string{source.String()}}}})
	started := time.Now()
	if err := a.SetDeviceGate(ctx, command); err != nil {
		t.Fatal(err)
	}
	if testutil.GateRoundTrip(c) || time.Since(started) > 3*time.Second {
		t.Fatal("gated connection not closed within three seconds")
	}
	allowed, err := testutil.GateTunnel(proxy, target, credential.Username, credential.Secret, nil)
	if err != nil {
		t.Fatal("allowed source failed")
	}
	defer allowed.Close()
	if !testutil.GateRoundTrip(allowed) {
		t.Fatal("another source was closed")
	}
}
