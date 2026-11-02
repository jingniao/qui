package xray

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// forwardInbound is the panel's port-forward inbound: a dokodemo-door listener
// with no credential and a native per-inbound counter.
func forwardInbound(id string, port int, enabled bool) desiredstate.Inbound {
	return desiredstate.Inbound{ID: id, Protocol: "forward", Listen: "127.0.0.1", Port: port, Enabled: enabled, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp,udp"}}}
}

// TestForwardStatsAllowlistRendersOnlyForwardInbounds pins the runtime metadata:
// enabled forward inbounds are the only inbound counters that reach the node
// total, and they never become accounts or online stats.
func TestForwardStatsAllowlistRendersOnlyForwardInbounds(t *testing.T) {
	state := testState(availablePort(t), 1)
	state.Inbounds = append(state.Inbounds, forwardInbound("z-forward", availablePort(t), true), forwardInbound("a-forward", availablePort(t), true), forwardInbound("off-forward", availablePort(t), false))
	state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "device-account", Secret: "stats-forward-password"}}
	state.Inbounds[0].CredentialIDs = []string{"credential"}
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), RenderOptions{StatsPort: 21000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rendered.StatsNodeInbounds, ",") != "a-forward,z-forward" {
		t.Fatal("forward allowlist is not the sorted enabled forward tags", rendered.StatsNodeInbounds)
	}
	if len(rendered.StatsAccounts) != 1 {
		t.Fatal("forward inbounds invented accounts", rendered.StatsAccounts)
	}
	for key := range rendered.StatsAccounts {
		if !strings.HasPrefix(key, "user>>>") {
			t.Fatal("forward inbound reached the account allowlist", key)
		}
	}
	var document configDocument
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Policy == nil || document.Policy.System == nil || !document.Policy.System.InboundUpload || !document.Policy.System.InboundDownload {
		t.Fatal("inbound statistics were not enabled")
	}
	if document.Policy.System.Upload || document.Policy.System.Download {
		t.Fatal("outbound statistics rendered without a WireGuard device")
	}
}

// TestForwardStatsAllowlistStaysEmptyWithoutForwards keeps user-only nodes and
// WireGuard's outbound counters exactly as they were.
func TestForwardStatsAllowlistStaysEmptyWithoutForwards(t *testing.T) {
	state := testState(availablePort(t), 1)
	state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{{ID: "peer", UserID: "user", DeviceID: "device", Protocol: "wireguard", Secret: "wireguard-secret"}}
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), RenderOptions{StatsPort: 21000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.StatsNodeInbounds) != 0 {
		t.Fatal("user node rendered forward counters", rendered.StatsNodeInbounds)
	}
	var document configDocument
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Policy == nil || document.Policy.System == nil || !document.Policy.System.InboundUpload || !document.Policy.System.InboundDownload || !document.Policy.System.Upload || !document.Policy.System.Download {
		t.Fatal("WireGuard outbound statistics changed")
	}
}
