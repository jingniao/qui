package singbox

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
)

// clashTotalsFixture serves the loopback Clash API surface the node-total reader
// uses: GET /connections behind the per-configuration Bearer secret.
type clashTotalsFixture struct {
	mu       sync.Mutex
	upload   uint64
	download uint64
}

func (f *clashTotalsFixture) set(upload, download uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upload, f.download = upload, download
}

func clashFixture(t *testing.T, secret string) (*clashTotalsFixture, string) {
	t.Helper()
	fixture := &clashTotalsFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fixture.mu.Lock()
		upload, download := fixture.upload, fixture.download
		fixture.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"uploadTotal": upload, "downloadTotal": download, "connections": []any{}, "memory": 1})
	}))
	t.Cleanup(server.Close)
	return fixture, strings.TrimPrefix(server.URL, "http://")
}

func clashAdapter(endpoint, secret string) *Adapter {
	return &Adapter{
		NodeID: "node", statsSecret: secret, process: &exec.Cmd{},
		current: core.RenderedConfig{NodeID: "node", Revision: 2, StatsEndpoint: endpoint},
	}
}

func TestNodeTrafficTotalsDiffClashCounters(t *testing.T) {
	fixture, endpoint := clashFixture(t, "secret-value")
	adapter := clashAdapter(endpoint, "secret-value")
	ctx := context.Background()
	fixture.set(100, 200)
	upload, download, err := adapter.NodeTrafficTotals(ctx)
	if err != nil || upload != 100 || download != 200 {
		t.Fatal("first node total did not report the core's own counters", upload, download, err)
	}
	fixture.set(150, 400)
	upload, download, err = adapter.NodeTrafficTotals(ctx)
	if err != nil || upload != 150 || download != 400 {
		t.Fatal("node total did not add the observed delta", upload, download, err)
	}
	// A core restart resets the Clash totals. The baseline restarts with them:
	// no negative delta, and the new process's bytes are not double counted.
	fixture.set(10, 20)
	upload, download, err = adapter.NodeTrafficTotals(ctx)
	if err != nil || upload != 160 || download != 420 {
		t.Fatal("node total mishandled a counter restart", upload, download, err)
	}
}

func TestNodeTrafficTotalsAreUnknownWithoutACloudApiSecret(t *testing.T) {
	_, endpoint := clashFixture(t, "secret-value")
	ctx := context.Background()
	if upload, download, err := clashAdapter(endpoint, "wrong-secret").NodeTrafficTotals(ctx); err == nil || upload != 0 || download != 0 {
		t.Fatal("wrong secret invented node totals", upload, download, err)
	}
	adapter := clashAdapter(endpoint, "secret-value")
	adapter.process = nil
	if upload, download, err := adapter.NodeTrafficTotals(ctx); err == nil || upload != 0 || download != 0 {
		t.Fatal("stopped core invented node totals", upload, download, err)
	}
	adapter = clashAdapter(endpoint, "")
	if upload, download, err := adapter.NodeTrafficTotals(ctx); err == nil || upload != 0 || download != 0 {
		t.Fatal("missing secret invented node totals", upload, download, err)
	}
}

func TestNodeTrafficTotalsReportUnreachableApi(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := listener.Addr().String()
	listener.Close()
	if upload, download, err := clashAdapter(closed, "secret-value").NodeTrafficTotals(context.Background()); err == nil || upload != 0 || download != 0 {
		t.Fatal("unreachable Clash API invented node totals", upload, download, err)
	}
}

// TestRealClashApiNodeTrafficTotalsCountForwardedBytes proves the node-total
// reader against the official core: after real traffic crosses the node, the
// Clash API totals the reader diffs must have grown by it.
func TestRealClashApiNodeTrafficTotalsCountForwardedBytes(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for real Clash API node totals")
	}
	ctx := context.Background()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	state := stateFixture(t, "http", port)
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
	adapter.ConfigureRuntime(core.RuntimeHooks{EnableStats: true})
	defer adapter.Close()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.StatsEndpoint == "" {
		t.Fatal("Clash API was not rendered")
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if upload, download, err := adapter.NodeTrafficTotals(ctx); err != nil || upload != 0 || download != 0 {
		t.Fatal("fresh core did not start at zero", upload, download, err)
	}
	payload := strings.Repeat("node-total", 2048)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	defer origin.Close()
	proxyURL, err := url.Parse("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	proxyURL.User = url.UserPassword("sing-user", state.Credentials[0].Secret)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal("native forwarding failed", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != payload {
		t.Fatal("proxied response incomplete", len(body), err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		upload, download, err := adapter.NodeTrafficTotals(ctx)
		if err == nil && upload+download >= uint64(len(payload)) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Clash API node totals did not count the forwarded traffic", upload, download, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
