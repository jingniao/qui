package xray

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// onlineStatsFixture serves the upstream StatsService surface the online reader
// uses: QueryStats for the cumulative counters, and GetStatsOnline for one
// user's online map.
type onlineStatsFixture struct {
	stats.UnimplementedStatsServiceServer
	mu     sync.Mutex
	online map[string]int64
	asked  []string
}

func (s *onlineStatsFixture) QueryStats(_ context.Context, request *stats.QueryStatsRequest) (*stats.QueryStatsResponse, error) {
	return &stats.QueryStatsResponse{}, status.Error(codes.InvalidArgument, "unexpected statistics query "+request.Pattern)
}

func (s *onlineStatsFixture) GetStatsOnline(_ context.Context, name string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, name)
	value, known := s.online[name]
	if !known {
		return 0, status.Error(codes.NotFound, name+" not found.")
	}
	return value, nil
}

type onlineStatsServer interface {
	GetStatsOnline(context.Context, string) (int64, error)
}

// registerOnlineStatsFixture starts a fake StatsService whose GetStatsOnline is
// registered by name next to the generated QueryStats method, exactly as the
// upstream service serves both. The generated package only carries QueryStats, so
// the fixture mirrors the reader's wire-compatible invocation.
func registerOnlineStatsFixture(t *testing.T, fixture *onlineStatsFixture) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	desc := stats.StatsService_ServiceDesc
	desc.Methods = append([]grpc.MethodDesc(nil), desc.Methods...)
	desc.Methods = append(desc.Methods, grpc.MethodDesc{MethodName: "GetStatsOnline", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		request := new(stats.QueryStatsRequest)
		if err := dec(request); err != nil {
			return nil, err
		}
		value, err := srv.(onlineStatsServer).GetStatsOnline(ctx, request.Pattern)
		if err != nil {
			return nil, err
		}
		return &stats.QueryStatsResponse{Stat: []*stats.Stat{{Name: request.Pattern, Value: value}}}, nil
	}})
	server := grpc.NewServer()
	server.RegisterService(&desc, fixture)
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); listener.Close(); <-done })
	return listener.Addr().String()
}

func onlineAdapter(endpoint string) *Adapter {
	return &Adapter{
		NodeID:     "node",
		statsEpoch: "online-epoch",
		// A running process is what makes the online statistics readable; no
		// process is started in this fixture.
		process: &exec.Cmd{},
		current: core.RenderedConfig{
			NodeID: "node", Revision: 4, StatsEndpoint: endpoint, OnlineStats: true,
			StatsAccounts: map[string]traffic.Counter{
				"user>>>credential:device-b": {Revision: 4, CredentialID: "device-b", UserID: "user", DeviceID: "device-b", Protocol: "vless"},
				"user>>>credential:device-a": {Revision: 4, CredentialID: "device-a", UserID: "user", DeviceID: "device-a", Protocol: "vless"},
				"outbound>>>_access_peer":    {Revision: 4, CredentialID: "wireguard-peer", UserID: "user", DeviceID: "device-wg", Protocol: "wireguard"},
			},
		},
	}
}

// TestRealOnlineStatisticsTrackAnAuthenticatedClient proves the online reader
// against the official core: one authenticated SOCKS5 client shows up as one
// online user while its session is open, and disappears when it closes. The
// cumulative per-user byte counters the panel already reads must move too.
//
// Upstream ignores loopback client addresses when it registers an online user
// (app/stats/online_map.go), so the test binds the inbound to the host's first
// non-loopback address when it has one and asserts the count there. Without such
// an address the reader still runs against the real API, and the expected count
// is zero.
func TestRealOnlineStatisticsTrackAnAuthenticatedClient(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for upstream online statistics")
	}
	ctx := context.Background()
	clientAddress := nonLoopbackIPv4()
	listen := "127.0.0.1"
	expectedOnline := uint64(0)
	if clientAddress != "" {
		listen, expectedOnline = clientAddress, 1
	} else {
		t.Log("no non-loopback address: upstream excludes loopback clients from online statistics")
	}
	state := testState(availablePort(t), 1)
	state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{{ID: "device-credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "device-account", Secret: "stats-test-password"}}
	state.Inbounds[0].CredentialIDs = []string{"device-credential"}
	state.Inbounds[0].Listen = listen
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID, EnableStats: true}
	defer adapter.Close()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if !rendered.OnlineStats {
		t.Fatal("official core version did not enable policy.levels.0.statsUserOnline")
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if sample, err := adapter.ConnectionSnapshot(ctx); err != nil || sample.Connections != 0 || len(sample.Accounts) != 1 {
		t.Fatal("idle core did not report zero online users", sample, err)
	}
	payload := strings.Repeat("online-data", 512)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	defer origin.Close()
	originAddress, err := net.ResolveTCPAddr("tcp", strings.TrimPrefix(origin.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	connection := socksConnect(t, net.JoinHostPort(state.Inbounds[0].Listen, strconv.Itoa(state.Inbounds[0].Port)), "device-account", "stats-test-password", originAddress)
	defer connection.Close()
	request, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	if err := request.Write(connection); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil {
		t.Fatal("statistics instrumentation broke forwarding", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != payload {
		t.Fatal("proxied response incomplete", len(body), err)
	}
	sample, err := adapter.ConnectionSnapshot(ctx)
	if err != nil || sample.Connections != expectedOnline || len(sample.Accounts) != 1 || sample.Accounts[0].CredentialID != "device-credential" || sample.Accounts[0].Connections != expectedOnline {
		t.Fatal("open session was not counted as one online user", sample, err)
	}
	counters, err := adapter.TrafficCounters(ctx)
	if err != nil || len(counters) != 1 || counters[0].CredentialID != "device-credential" || counters[0].UploadBytes <= 0 || counters[0].DownloadBytes <= 0 {
		t.Fatal("per-user delta missing next to the online count", counters, err)
	}
	connection.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sample, err = adapter.ConnectionSnapshot(ctx)
		if err == nil && sample.Connections == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed session stayed online", sample, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// socksConnect performs the username/password SOCKS5 handshake and opens a
// CONNECT tunnel to the origin, leaving the session open for the caller.
func socksConnect(t *testing.T, endpoint, username, password string, origin *net.TCPAddr) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	fail := func(what string, value any) {
		connection.Close()
		t.Fatalf("socks5 %s failed: %v", what, value)
	}
	if _, err := connection.Write([]byte{5, 1, 2}); err != nil {
		fail("greeting", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[0] != 5 || reply[1] != 2 {
		fail("method negotiation", reply)
	}
	auth := append([]byte{1, byte(len(username))}, username...)
	auth = append(auth, byte(len(password)))
	auth = append(auth, password...)
	if _, err := connection.Write(auth); err != nil {
		fail("authentication", err)
	}
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		fail("credential check", reply)
	}
	ip := origin.IP.To4()
	if ip == nil {
		fail("origin address", origin)
	}
	request := append([]byte{5, 1, 0, 1}, ip...)
	request = append(request, byte(origin.Port>>8), byte(origin.Port))
	if _, err := connection.Write(request); err != nil {
		fail("connect request", err)
	}
	response := make([]byte, 10)
	if _, err := io.ReadFull(connection, response); err != nil || response[1] != 0 {
		fail("connect reply", response)
	}
	connection.SetDeadline(time.Time{})
	return connection
}

// nonLoopbackIPv4 returns the host's first non-loopback IPv4 address, or "" when
// the host has none. Upstream's online map ignores loopback client addresses.
func nonLoopbackIPv4() string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		return ip.String()
	}
	return ""
}

func TestOnlineSnapshotReadsUpstreamPerUserCounts(t *testing.T) {
	fixture := &onlineStatsFixture{online: map[string]int64{"user>>>credential:device-a>>>online": 3}}
	endpoint := registerOnlineStatsFixture(t, fixture)
	sample, err := onlineAdapter(endpoint).ConnectionSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sample.NodeID != "node" || sample.Revision != 4 || sample.CoreEpoch != "online-epoch" || sample.Scope != traffic.ConnectionScope {
		t.Fatal("online snapshot lost its process identity", sample)
	}
	if len(sample.Accounts) != 2 || sample.Accounts[0].CredentialID != "device-a" || sample.Accounts[0].Connections != 3 || sample.Accounts[1].CredentialID != "device-b" || sample.Accounts[1].Connections != 0 {
		t.Fatal("online snapshot did not report both user accounts", sample.Accounts)
	}
	if sample.Connections != 3 {
		t.Fatal("online snapshot total does not match its accounts", sample.Connections)
	}
	if err := sample.Validate(); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	sort.Strings(fixture.asked)
	if strings.Join(fixture.asked, ",") != "user>>>credential:device-a>>>online,user>>>credential:device-b>>>online" {
		t.Fatal("reader did not ask the upstream online maps", fixture.asked)
	}
}

func TestOnlineSnapshotIsUnknownWithoutOnlineStatistics(t *testing.T) {
	fixture := &onlineStatsFixture{online: map[string]int64{}}
	endpoint := registerOnlineStatsFixture(t, fixture)
	adapter := onlineAdapter(endpoint)
	adapter.current.OnlineStats = false
	if sample, err := adapter.ConnectionSnapshot(context.Background()); err == nil || sample.Connections != 0 {
		t.Fatal("older core without online statistics invented a count", sample, err)
	}
	adapter = onlineAdapter(endpoint)
	adapter.process = nil
	if sample, err := adapter.ConnectionSnapshot(context.Background()); err == nil || sample.Connections != 0 {
		t.Fatal("stopped core invented a count", sample, err)
	}
}

func TestOnlineSnapshotReportsUnreachableApiAsUnknown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if sample, err := onlineAdapter(endpoint).ConnectionSnapshot(ctx); err == nil || sample.Connections != 0 {
		t.Fatal("unreachable statistics API invented a count", sample, err)
	}
}

func TestOnlineSnapshotRejectsInvalidUpstreamCounts(t *testing.T) {
	fixture := &onlineStatsFixture{online: map[string]int64{"user>>>credential:device-a>>>online": 100000001}}
	endpoint := registerOnlineStatsFixture(t, fixture)
	if sample, err := onlineAdapter(endpoint).ConnectionSnapshot(context.Background()); err == nil || sample.Connections != 0 {
		t.Fatal("out-of-range online count accepted", sample, err)
	}
}
