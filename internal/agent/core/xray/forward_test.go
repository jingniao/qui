package xray

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestForwardGolden(t *testing.T) {
	s := goldenScenarios()[0].state
	s.Credentials = nil
	s.Users = nil
	s.Inbounds[0].Protocol = "forward"
	s.Inbounds[0].CredentialIDs = nil
	s.Inbounds[0].Settings = desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp,udp"}}
	r, err := RenderAt(context.Background(), s, goldenAt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "forward.json")
	if os.Getenv("UPDATE_FORWARD_GOLDEN") == "1" {
		if err = os.WriteFile(path, r.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(r.Data) {
		t.Fatalf("forward golden mismatch: %s", r.Data)
	}
	// The golden renders no statistics, so the node-counter allowlist and the
	// inbound statistics policy are asserted on an instrumented render. Inbound
	// counters stay on, and outbound counters stay off without a WireGuard
	// device, so the JSON above is unchanged by this metadata.
	instrumented, err := renderWithOptions(context.Background(), s, goldenAt, goldenOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(instrumented.StatsNodeInbounds) != 1 || instrumented.StatsNodeInbounds[0] != s.Inbounds[0].ID {
		t.Fatal("forward inbound is not the only node counter", instrumented.StatsNodeInbounds)
	}
	if len(instrumented.StatsAccounts) != 0 {
		t.Fatal("a forward inbound invented an account", instrumented.StatsAccounts)
	}
	var document configDocument
	if err := json.Unmarshal(instrumented.Data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Policy == nil || document.Policy.System == nil || !document.Policy.System.InboundUpload || !document.Policy.System.InboundDownload || document.Policy.System.Upload || document.Policy.System.Download {
		t.Fatal("forward statistics policy changed")
	}
	if binary := os.Getenv("XRAY_BINARY"); binary != "" {
		a := &Adapter{Path: binary, Directory: t.TempDir()}
		if err = a.Validate(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealForward(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("XRAY_BINARY is unset")
	}
	testutil.RealForward(t, binary, []string{"run", "-config"}, func(s desiredstate.State) ([]byte, error) {
		r, e := RenderAt(context.Background(), s, goldenAt)
		return r.Data, e
	})
}

// forwardEcho starts TCP and UDP echo listeners on one port, the way
// testutil.RealForward does, so one forward inbound can reach both transports.
func forwardEcho(t *testing.T) int {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tcp.Close() })
	port := tcp.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udp.Close() })
	go func() {
		for {
			connection, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() { defer connection.Close(); io.Copy(connection, connection) }()
		}
	}()
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, address, err := udp.ReadFrom(buffer)
			if err != nil {
				return
			}
			udp.WriteTo(buffer[:n], address)
		}
	}()
	return port
}

// echo pushes one payload through the forward listener and closes the session,
// so the native counters see a finished connection rather than a live one.
func echo(t *testing.T, network, address, payload string) {
	t.Helper()
	connection, err := net.DialTimeout(network, address, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, got); err != nil || string(got) != payload {
		t.Fatalf("%s echo %q: %v", network, got, err)
	}
}

// TestRealForwardNodeCountersCountForwardedBytes proves the native Xray inbound
// counters reach the node total through the real adapter: forwarded bytes alone
// count, user inbound bytes are never counted twice, and a restart starts a new
// `.node` epoch. A fake StatsService is parser evidence, not this.
func TestRealForwardNodeCountersCountForwardedBytes(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("XRAY_BINARY is unset")
	}
	ctx := context.Background()
	target := forwardEcho(t)
	port := availablePort(t)
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: testNodeID, Logging: desiredstate.Logging{DomainOnly: true},
		Inbounds:  []desiredstate.Inbound{forwardInbound("forward", port, true)},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	state.Inbounds[0].Settings = desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "127.0.0.1", Port: target, Network: "tcp,udp"}}
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: testNodeID, EnableStats: true}
	var saved []traffic.Counter
	adapter.TrafficSink = func(_ context.Context, counters []traffic.Counter) error {
		saved = append(saved, append([]traffic.Counter(nil), counters...)...)
		return nil
	}
	defer adapter.Close()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.StatsNodeInbounds) != 1 || rendered.StatsNodeInbounds[0] != "forward" {
		t.Fatal("forward inbound is not the node counter", rendered.StatsNodeInbounds)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	payload := "forward-node-counter"
	echo(t, "tcp", address, payload)
	echo(t, "udp", address, payload)
	total := waitForNodeBytes(t, adapter)
	if total.Upload+total.Download < uint64(len(payload)) {
		t.Fatal("native forward counters did not reach the node total", total)
	}
	if err := adapter.SampleTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	var epoch string
	for _, counter := range saved {
		if counter.Kind != "node" {
			t.Fatal("a credential-free forward emitted a user counter", counter)
		}
		epoch = counter.CoreEpoch
	}
	if !strings.HasSuffix(epoch, ".node") || len(saved) == 0 {
		t.Fatal("forward samples have no node epoch", saved)
	}
	// Reads never reset the native counters, so the next sample is cumulative.
	var before uint64
	for _, counter := range saved {
		before = uint64(counter.UploadBytes + counter.DownloadBytes)
	}
	if err := adapter.SampleTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	var after uint64
	for _, counter := range saved[len(saved)-1:] {
		after = uint64(counter.UploadBytes + counter.DownloadBytes)
		if counter.Kind != "node" || counter.CoreEpoch != epoch {
			t.Fatal("repeated sample left the process epoch", counter)
		}
	}
	if after < before {
		t.Fatal("repeated sample lost cumulative bytes", before, after)
	}
	restartMixed(t, adapter, state, port, epoch)
}

// waitForNodeBytes polls the live node total to a bounded deadline: native
// counters become visible shortly after a session closes, and the payload is
// never asserted byte-for-byte.
func waitForNodeBytes(t *testing.T, adapter *Adapter) core.StatsBatch {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		total, err := adapter.CollectStats(context.Background())
		if err == nil && total.Upload+total.Download > 0 {
			return total
		}
		if time.Now().After(deadline) {
			t.Fatal("node total stayed empty", total, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// restartMixed adds a user inbound next to the forward. The node total must
// then be the account bytes plus the forwarded bytes and never the user inbound
// counter again; restarting the core starts a new `.node` epoch at zero.
func restartMixed(t *testing.T, adapter *Adapter, state desiredstate.State, port int, epoch string) {
	t.Helper()
	ctx := context.Background()
	socksPort := availablePort(t)
	next := state
	next.Revision = 2
	next.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	next.Credentials = []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "node-account", Secret: "node-counter-password"}}
	next.Inbounds = append(append([]desiredstate.Inbound(nil), state.Inbounds...), desiredstate.Inbound{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: socksPort, Enabled: true, CredentialIDs: []string{"credential"}, DefaultOutbound: "direct"})
	rendered, err := adapter.Render(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	body := strings.Repeat("user-node", 8192)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
	defer origin.Close()
	proxyURL, err := url.Parse("socks5://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)))
	if err != nil {
		t.Fatal(err)
	}
	proxyURL.User = url.UserPassword("node-account", "node-counter-password")
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 10 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal("user inbound failed", err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	echo(t, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), "mixed-forward")
	deadline := time.Now().Add(10 * time.Second)
	for {
		accounts, accountErr := adapter.TrafficCounters(ctx)
		var sum uint64
		for _, counter := range accounts {
			sum += uint64(counter.UploadBytes + counter.DownloadBytes)
		}
		total, totalErr := adapter.CollectStats(ctx)
		if accountErr == nil && sum > 0 && totalErr == nil && total.Upload+total.Download > sum {
			// Forwarded bytes here are a few dozen; anything near a second
			// copy of the account bytes is the user inbound counted again.
			if total.Upload+total.Download >= sum+sum/2 {
				t.Fatal("user inbound bytes were counted twice in the node total", total, sum)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mixed accounting never added the forward bytes", total, sum, accountErr, totalErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if adapter.statsEpoch == "" || strings.HasPrefix(epoch, adapter.statsEpoch) {
		t.Fatal("restart kept the previous process epoch", epoch, adapter.statsEpoch)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		total, err := adapter.CollectStats(ctx)
		if err == nil {
			if total.Upload+total.Download != 0 {
				t.Fatal("restarted core did not start at zero", total)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restarted statistics stayed unavailable", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
