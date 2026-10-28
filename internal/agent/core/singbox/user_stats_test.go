package singbox

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/spool"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"
)

type fakeUserStats struct {
	stats.UnimplementedStatsServiceServer
	mu     sync.Mutex
	values []*stats.Stat
	calls  int
	err    error
}

func (s *fakeUserStats) QueryStats(_ context.Context, request *stats.QueryStatsRequest) (*stats.QueryStatsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if request.Reset_ || request.Pattern != "" {
		return nil, errors.New("reader must issue one non-resetting empty query")
	}
	return &stats.QueryStatsResponse{Stat: s.values}, s.err
}
func fakeStatsEndpoint(t *testing.T, s *fakeUserStats) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	desc := stats.StatsService_ServiceDesc
	desc.ServiceName = "v2ray.core.app.stats.command.StatsService"
	server.RegisterService(&desc, s)
	go server.Serve(l)
	t.Cleanup(server.Stop)
	return l.Addr().String()
}
func statsPair(name string, up, down int64) []*stats.Stat {
	return []*stats.Stat{{Name: name + ">>>traffic>>>uplink", Value: up}, {Name: name + ">>>traffic>>>downlink", Value: down}}
}
func TestUserStatsSingleQueryCursorRestart(t *testing.T) {
	ctx := context.Background()
	s := &fakeUserStats{}
	endpoint := fakeStatsEndpoint(t, s)
	state := stateFixture(t, "vless", 1080)
	rendered, err := renderWithOptions(ctx, state, goldenAt, renderOptions{userStatsPort: 15403})
	if err != nil {
		t.Fatal(err)
	}
	rendered.UserStatsEndpoint = endpoint
	// An outbound account must never trigger the Xray reader's second query.
	rendered.StatsAccounts["outbound>>>unused"] = traffic.Counter{CredentialID: "unused"}
	directory := t.TempDir()
	queue, err := spool.Open(ctx, directory, state.NodeID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { queue.Close() }()
	var sources core.CounterSources
	read := func(epoch string, up, down int64) []traffic.Counter {
		t.Helper()
		s.mu.Lock()
		before := s.calls
		s.values = append(statsPair("user>>>credential", up, down), statsPair("inbound>>>entry", 10000, 20000)...)
		s.mu.Unlock()
		counters, err := readCounters(ctx, rendered, epoch, &sources)
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		calls := s.calls
		s.mu.Unlock()
		if calls != before+1 || len(counters) != 1 || counters[0].UploadBytes != up || counters[0].DownloadBytes != down {
			t.Fatal("single query/filter/cumulative contract", calls, counters)
		}
		return counters
	}
	save := func(c []traffic.Counter) {
		t.Helper()
		if err := queue.SaveTraffic(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	save(read("core-1", 100, 1000))
	save(read("core-1", 100, 1000))
	queue.Close()
	queue, err = spool.Open(ctx, directory, state.NodeID, 0)
	if err != nil {
		t.Fatal(err)
	}
	sources = core.CounterSources{} // Agent restart retains the durable spool cursor.
	save(read("core-1", 100, 1000))
	batch, err := queue.PendingTraffic(ctx)
	if err != nil || batch == nil || batch.Counters[0].UploadBytes != 100 {
		t.Fatal(batch, err)
	}
	if err := queue.AckTraffic(ctx, batch.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := queue.PendingTraffic(ctx); err != nil || b != nil {
		t.Fatal("restart duplicated batch", b, err)
	}
	save(read("core-1", 150, 1400))
	batch, err = queue.PendingTraffic(ctx)
	if err != nil || batch == nil || batch.Counters[0].UploadBytes != 150 || batch.Counters[0].DownloadBytes != 1400 {
		t.Fatal(batch, err)
	}
	queue.AckTraffic(ctx, batch.ID)
	// Failed persistence must leave the cursor unchanged so the next read retries.
	counters := read("core-1", 180, 1500)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := queue.SaveTraffic(cancelled, counters); err == nil {
		t.Fatal("cancelled write accepted")
	}
	save(read("core-1", 180, 1500))
	batch, err = queue.PendingTraffic(ctx)
	if err != nil || batch == nil || batch.Counters[0].UploadBytes != 180 {
		t.Fatal("failed write lost sample", batch, err)
	}
	queue.AckTraffic(ctx, batch.ID)
	save(read("core-2", 20, 200)) // A core restart begins a separate cumulative epoch.
	batch, err = queue.PendingTraffic(ctx)
	if err != nil || batch == nil || batch.Counters[0].CoreEpoch != "core-2" || batch.Counters[0].DownloadBytes != 200 {
		t.Fatal(batch, err)
	}
	queue.AckTraffic(ctx, batch.ID)
	save(read("core-2", 20, 200))
	if b, err := queue.PendingTraffic(ctx); err != nil || b != nil {
		t.Fatal("core restart double counted", b, err)
	}
	if err := queue.SaveTraffic(ctx, read("core-2", 19, 200)); err == nil {
		t.Fatal("same epoch decrease accepted")
	}
}

func TestUserStatsLazyAndInvalidResponses(t *testing.T) {
	s := &fakeUserStats{}
	endpoint := fakeStatsEndpoint(t, s)
	config := core.RenderedConfig{NodeID: "node", Revision: 1, UserStatsEndpoint: endpoint, StatsAccounts: map[string]traffic.Counter{"user>>>u": {Revision: 1, CredentialID: "c", UserID: "u", DeviceID: "d", Protocol: "vless"}}}
	var source core.CounterSources
	if c, err := readCounters(context.Background(), config, "epoch", &source); err != nil || len(c) != 0 {
		t.Fatal("lazy user invented a zero", c, err)
	}
	for _, values := range [][]*stats.Stat{statsPair("user>>>u", 1, 2), {{Name: "user>>>u>>>traffic>>>uplink", Value: 1}}, statsPair("user>>>u", -1, 2), append(statsPair("user>>>u", 1, 2), statsPair("user>>>u", 1, 2)...), {}} {
		s.mu.Lock()
		s.values = values
		s.mu.Unlock()
		_, err := readCounters(context.Background(), config, "epoch", &source)
		if len(values) == 2 && values[0].Value == 1 {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("invalid/disappeared source accepted", values)
		}
	}
	config.UserStatsEndpoint = "0.0.0.0:12345"
	if _, err := readCounters(context.Background(), config, "epoch", nil); err == nil {
		t.Fatal("non-loopback accepted")
	}
}

func TestUserStatsGoldenAndLoopbackOnly(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "stats-official"
		options := goldenOptions
		if enabled {
			name = "stats-v2ray"
			options.userStatsPort = 15403
		}
		rendered, err := renderWithOptions(context.Background(), goldenScenarios()[1].state, goldenAt, options)
		if err != nil {
			t.Fatal(err)
		}
		golden, err := os.ReadFile(filepath.Join("testdata", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSuffix(string(golden), "\n") != string(rendered.Data) {
			t.Fatalf("golden mismatch %s:\n%s", name, rendered.Data)
		}
		var doc document
		if err := json.Unmarshal(rendered.Data, &doc); err != nil {
			t.Fatal(err)
		}
		if enabled {
			if doc.Experimental.V2rayAPI.Listen != "127.0.0.1:15403" || !reflect.DeepEqual(doc.Experimental.V2rayAPI.Stats.Users, []string{"device-credential"}) || !reflect.DeepEqual(doc.Experimental.V2rayAPI.Stats.Inbounds, []string{"entry"}) {
				t.Fatal(doc.Experimental)
			}
		} else if doc.Experimental.V2rayAPI != nil || rendered.UserStatsEndpoint != "" {
			t.Fatal("official build rendered user stats")
		}
		if binary := os.Getenv("SINGBOX_BINARY"); binary != "" {
			caps, err := Detect(context.Background(), binary)
			if err != nil {
				t.Fatal(err)
			}
			if !enabled || caps.SingboxUserStats {
				if err := (&Adapter{Path: binary, Directory: t.TempDir()}).Validate(context.Background(), rendered); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestUserStatsCapabilityFreePortAndOfficialFallback(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a := &Adapter{statsChecked: true, nativeClashAPI: true, v2rayAPI: enabled}
		a.ConfigureRuntime(core.RuntimeHooks{EnableStats: true})
		state := stateFixture(t, "socks5", freePort(t))
		rendered, err := a.Render(context.Background(), state)
		if err != nil {
			t.Fatal(err)
		}
		if a.SupportsAccountTelemetry() != enabled {
			t.Fatal("wrong telemetry capability")
		}
		if enabled {
			host, port, err := net.SplitHostPort(rendered.UserStatsEndpoint)
			if err != nil || host != "127.0.0.1" || port == "0" {
				t.Fatal(rendered.UserStatsEndpoint, err)
			}
			l, err := net.Listen("tcp", rendered.UserStatsEndpoint)
			if err != nil {
				t.Fatal("stats port was not free", err)
			}
			l.Close()
		} else if _, err := a.TrafficCounters(context.Background()); !errors.Is(err, ErrTelemetryUnavailable) {
			t.Fatal("official stats not unknown", err)
		}
		caps := &pb.Capabilities{CoreName: "sing-box", SingboxUserStats: enabled}
		if SupportsV2rayAPI(caps) != enabled {
			t.Fatal("capability not respected")
		}
	}
	if !buildTags(strings.Replace(officialVersionBanner, "with_quic", "with_quic,with_v2ray_api", 1))["with_v2ray_api"] {
		t.Fatal("tag not parsed")
	}
}

func TestUserStatsFinalSampleFailureKeepsCore(t *testing.T) {
	s := &fakeUserStats{values: statsPair("user>>>u", 1, 2)}
	endpoint := fakeStatsEndpoint(t, s)
	a := &Adapter{process: &exec.Cmd{}, done: make(chan error), statsEpoch: "epoch", current: core.RenderedConfig{NodeID: "node", UserStatsEndpoint: endpoint, StatsAccounts: map[string]traffic.Counter{"user>>>u": {Revision: 1, CredentialID: "c", UserID: "u", DeviceID: "d", Protocol: "vless"}}}}
	a.ConfigureRuntime(core.RuntimeHooks{TrafficSink: func(context.Context, []traffic.Counter) error { return errors.New("disk full") }})
	if err := a.stopLocked(); err == nil || a.process == nil {
		t.Fatal("failed persistence allowed replacement", err)
	}
}

func TestUserStatsIncludesEveryBoundUserAndExcludesRelay(t *testing.T) {
	state := goldenScenarios()[0].state
	state.Inbounds = nil
	state.Credentials = nil
	for i, protocol := range []string{"http", "socks5", "shadowsocks", "vless", "trojan", "hysteria2", "tuic"} {
		name := "account-" + protocol
		state.Credentials = append(state.Credentials, desiredstate.Credential{ID: name, UserID: "user", DeviceID: "identity", Protocol: protocol, Username: "login-" + protocol})
		state.Inbounds = append(state.Inbounds, desiredstate.Inbound{ID: protocol, Protocol: protocol, Enabled: true, Port: 20000 + i, CredentialIDs: []string{name}})
	}
	state.Credentials = append(state.Credentials, desiredstate.Credential{ID: "relay", Protocol: "trojan", Internal: true}, desiredstate.Credential{ID: "unbound", UserID: "user", DeviceID: "identity", Protocol: "vless"})
	state.Inbounds = append(state.Inbounds, desiredstate.Inbound{ID: "relay", Protocol: "trojan", Port: 20100, Enabled: true, CredentialIDs: []string{"relay"}})
	doc := document{}
	rendered := core.RenderedConfig{}
	if err := configureUserStats(&doc, state, 15403, &rendered); err != nil {
		t.Fatal(err)
	}
	if len(doc.Experimental.V2rayAPI.Stats.Users) != 7 || len(doc.Experimental.V2rayAPI.Stats.Inbounds) != 7 || len(rendered.StatsAccounts) != 7 {
		t.Fatal("missing or internal statistics account", doc.Experimental.V2rayAPI, rendered.StatsAccounts)
	}
	for _, protocol := range []string{"http", "socks5", "shadowsocks", "vless", "trojan", "hysteria2", "tuic"} {
		name := "account-" + protocol
		if protocol == "http" || protocol == "socks5" {
			name = "login-" + protocol
		}
		if rendered.StatsAccounts["user>>>"+name].CredentialID != "account-"+protocol {
			t.Fatal("wrong native user name", protocol)
		}
	}
	state.Credentials[1].Username = state.Credentials[0].Username
	state.Credentials[1].UserID = "foreign-user"
	if err := configureUserStats(&document{}, state, 15403, &core.RenderedConfig{}); err == nil {
		t.Fatal("ambiguous username accepted")
	}
	state.Inbounds[0].Port = 15403
	if err := configureUserStats(&document{}, state, 15403, &core.RenderedConfig{}); err == nil {
		t.Fatal("private port collision accepted")
	}
}
