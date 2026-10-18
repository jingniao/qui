package singbox

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRealSingboxAccessRecordsRejectedAndAllowedConnections proves the Agent's
// sing-box access source with the official core: a connection the policy
// rejects and a connection the default route allows both become access rollups
// with the actual rule, action, domain and user of this node.
//
// The source is the core's own log stream (see internal/agent/collector's
// sing-box writer). The Clash API `/connections` endpoint lists only open
// connections, so a rejected connection - and any connection that opens and
// closes between polls - is never listed there, and its metadata names no user
// (plan D5). It is therefore used for node totals only (NodeTrafficTotals).
func TestRealSingboxAccessRecordsRejectedAndAllowedConnections(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for real access collection")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	state := stateFixture(t, "socks5", port)
	state.Policies = []desiredstate.Policy{{ID: "deny-blocked", Priority: 10, Enabled: true, Match: desiredstate.Match{Domains: []string{"blocked.example"}}, Action: desiredstate.Action{Type: "Block"}}}
	events := make(chan access.Rollup, 16)
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
	adapter.ConfigureRuntime(core.RuntimeHooks{AccessSink: func(record access.Rollup) error { events <- record; return nil }})
	defer adapter.Close()
	rendered, err := adapter.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(context.Background(), rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "access-response") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(port))
	proxyURL.User = url.UserPassword("sing-user", state.Credentials[0].Secret)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	if response, err := client.Get("http://blocked.example/private"); err == nil {
		response.Body.Close()
		t.Fatal("rejected domain passed the core")
	}
	response, err := client.Get(strings.Replace(origin.URL, "127.0.0.1", "localhost", 1))
	if err != nil {
		t.Fatal("allowed request failed", err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	seen := map[string]access.Rollup{}
	deadline := time.Now().Add(10 * time.Second)
	for len(seen) < 2 && time.Now().Before(deadline) {
		select {
		case record := <-events:
			seen[record.Domain] = record
		case <-time.After(50 * time.Millisecond):
		}
	}
	blocked, ok := seen["blocked.example"]
	if !ok {
		t.Fatalf("rejected connection not recorded: %+v", seen)
	}
	if blocked.Allowed || blocked.Action != "Block" || blocked.RuleID != "deny-blocked" || blocked.TargetPort != 80 || blocked.Network != "tcp" || blocked.UserID != "user" || blocked.DeviceID != "device" || blocked.InboundProtocol != "socks5" || blocked.NodeID != state.NodeID || blocked.TrafficKnown {
		t.Fatalf("rejected connection not attributed: %+v", blocked)
	}
	allowed, ok := seen["localhost"]
	if !ok {
		t.Fatalf("allowed connection not recorded: %+v", seen)
	}
	if !allowed.Allowed || allowed.Action != "Direct" || allowed.OutboundID != "direct" || allowed.OutboundProtocol != "direct" || allowed.UserID != "user" || allowed.DeviceID != "device" || allowed.RuleID != "" || allowed.TrafficKnown {
		t.Fatalf("allowed connection not attributed: %+v", allowed)
	}
}
