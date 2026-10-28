package xray

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
)

func TestRealDeviceGateXrayScopeEstablishedBalancerAndFailedReload(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("awaiting CI/Owner run: XRAY_BINARY")
	}
	ctx := context.Background()
	source := testutil.GateSource(t)
	target := testutil.GateEcho(t)
	s := testState(availablePort(t), 1)
	s.Inbounds[0].Protocol = "http"
	s.Users = []desiredstate.User{{ID: "u1", Enabled: true}, {ID: "u2", Enabled: true}}
	s.Credentials = []desiredstate.Credential{{ID: "c1", UserID: "u1", DeviceID: "d1", Username: "first", Secret: "first-password", Protocol: "http"}, {ID: "c2", UserID: "u2", DeviceID: "d2", Username: "second", Secret: "second-password", Protocol: "http"}}
	s.Inbounds[0].CredentialIDs = []string{"c1", "c2"}
	a := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: s.NodeID, EnableStats: true}
	defer a.Close()
	rendered, err := a.Render(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the official loader's balancer-preservation behavior with a
	// stable one-member pool; production route compilation is tested separately.
	var doc configDocument
	if json.Unmarshal(rendered.Data, &doc) != nil {
		t.Fatal("invalid render")
	}
	doc.Routing.Balancers = []balancerConfig{{Tag: "gate-pool", Selector: []string{"direct"}, FallbackTag: "direct", Strategy: balancerStrategy{Type: "roundRobin"}}}
	last := len(doc.Routing.Rules) - 1
	doc.Routing.Rules[last].OutboundTag = ""
	doc.Routing.Rules[last].BalancerTag = "gate-pool"
	rendered.Data, _ = json.Marshal(doc)
	if result, err := a.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Inbounds[0].Port))
	open := func(user, password string, ip net.IP) net.Conn {
		c, err := testutil.GateTunnel(proxy, target, user, password, ip)
		if err != nil {
			t.Fatal("tunnel failed")
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	established := open("first", "first-password", source)
	if !testutil.GateRoundTrip(established) {
		t.Fatal("baseline balancer traffic failed")
	}
	command, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []map[string]any{{"credential_id": "c1", "keys": []string{source.String()}}}})
	if err := a.SetDeviceGate(ctx, command); err != nil {
		t.Fatal(err)
	}
	if !testutil.GateRoundTrip(established) {
		t.Fatal("established connection was cut")
	}
	c, err := testutil.GateTunnel(proxy, target, "first", "first-password", source)
	if err == nil {
		defer c.Close()
		if testutil.GateRoundTrip(c) {
			t.Fatal("gated user/source admitted")
		}
	}
	if !testutil.GateRoundTrip(open("second", "second-password", source)) || !testutil.GateRoundTrip(open("first", "first-password", nil)) {
		t.Fatal("gate affected another user or source")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if os.WriteFile(bad, []byte(`{"routing":{"rules":[{"type":"field","ruleTag":"bad","balancerTag":"missing"}]}}`), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	first := true
	a.gateRunner = func(ctx context.Context, args []string, out io.Writer) error {
		if args[1] == "adrules" && first {
			first = false
			args = append([]string(nil), args...)
			args[len(args)-1] = bad
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Stdout = out
		cmd.Stderr = io.Discard
		return cmd.Run()
	}
	// Force a different payload, so the unchanged-payload optimization cannot
	// skip the intentionally non-atomic failed reload.
	a.mu.Lock()
	a.gateDigest = ""
	a.mu.Unlock()
	if err := a.SetDeviceGate(ctx, command); err == nil {
		t.Fatal("forced invalid balancer reload succeeded")
	}
	if a.gateRecovery {
		t.Fatal("gate-free routing could not be verified; inspect CI core compatibility")
	}
	if !testutil.GateRoundTrip(open("first", "first-password", source)) {
		t.Fatal("routing/balancer traffic not restored after failed reload")
	}
}
