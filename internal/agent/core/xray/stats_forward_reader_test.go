package xray

import (
	"context"
	"math"
	"net"
	"os/exec"
	"sync"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// forwardStatsFixture serves the upstream StatsService surface the reader uses
// and records every pattern, so a test can assert which queries were issued and
// that a cumulative counter was never reset.
type forwardStatsFixture struct {
	stats.UnimplementedStatsServiceServer
	mu        sync.Mutex
	responses map[string][]*stats.Stat
	patterns  []string
	fail      bool
}

func (s *forwardStatsFixture) QueryStats(_ context.Context, request *stats.QueryStatsRequest) (*stats.QueryStatsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.Reset_ {
		return nil, status.Error(codes.InvalidArgument, "cumulative counters must never be reset")
	}
	s.patterns = append(s.patterns, request.Pattern)
	if s.fail {
		return nil, status.Error(codes.Unavailable, "statistics unavailable")
	}
	values, known := s.responses[request.Pattern]
	if !known {
		return nil, status.Error(codes.InvalidArgument, "unexpected statistics query "+request.Pattern)
	}
	return &stats.QueryStatsResponse{Stat: values}, nil
}

func (s *forwardStatsFixture) set(pattern string, values ...*stats.Stat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses[pattern] = values
}

func (s *forwardStatsFixture) queried() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.patterns...)
}

func forwardStatsServer(t *testing.T, responses map[string][]*stats.Stat) (*forwardStatsFixture, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &forwardStatsFixture{responses: responses}
	server := grpc.NewServer()
	stats.RegisterStatsServiceServer(server, fixture)
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); listener.Close(); <-done })
	return fixture, listener.Addr().String()
}

func stat(name string, value int64) *stats.Stat { return &stats.Stat{Name: name, Value: value} }

// forwardStatsAdapter is the production sampling path with a recording sink and
// a stand-in process handle; only the native statistics RPC is real.
func forwardStatsAdapter(configuration core.RenderedConfig) (*Adapter, *[][]traffic.Counter) {
	var batches [][]traffic.Counter
	adapter := &Adapter{process: &exec.Cmd{}, statsEpoch: "epoch", current: configuration}
	adapter.TrafficSink = func(_ context.Context, counters []traffic.Counter) error {
		batches = append(batches, append([]traffic.Counter(nil), counters...))
		return nil
	}
	return adapter, &batches
}

func forwardConfig(endpoint string, accounts map[string]traffic.Counter, forwards ...string) core.RenderedConfig {
	return core.RenderedConfig{NodeID: "node", Revision: 1, StatsEndpoint: endpoint, StatsAccounts: accounts, StatsNodeInbounds: forwards}
}

func TestStatsReaderCountsForwardInboundBytesOnce(t *testing.T) {
	fixture, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{
		"user>>>": {},
		"inbound>>>": {
			stat("inbound>>>forward>>>traffic>>>uplink", 120),
			stat("inbound>>>forward>>>traffic>>>downlink", 340),
			stat("inbound>>>user-in>>>traffic>>>uplink", 999),
			stat("inbound>>>user-in>>>traffic>>>downlink", 999),
			stat("inbound>>>_stats_api_>>>traffic>>>uplink", 1),
			stat("inbound>>>_stats_api_>>>traffic>>>downlink", 1),
		},
	})
	adapter, batches := forwardStatsAdapter(forwardConfig(endpoint, map[string]traffic.Counter{}, "forward"))
	if err := adapter.SampleTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*batches) != 2 || len((*batches)[0]) != 0 {
		t.Fatal("forward bytes became an account sample", *batches)
	}
	node := (*batches)[1]
	if len(node) != 1 || node[0].Kind != "node" || node[0].Core != "xray" || node[0].CoreEpoch != "epoch.node" || node[0].UploadBytes != 120 || node[0].DownloadBytes != 340 {
		t.Fatal("forward inbound bytes are missing from the node sample", node)
	}
	if patterns := fixture.queried(); len(patterns) != 2 || patterns[0] != "user>>>" || patterns[1] != "inbound>>>" {
		t.Fatal("unexpected statistics queries", patterns)
	}
}

func TestStatsReaderAddsAccountsAndForwards(t *testing.T) {
	fixture, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{
		"user>>>":            {stat("user>>>account>>>traffic>>>uplink", 10), stat("user>>>account>>>traffic>>>downlink", 20)},
		"outbound>>>_access": {stat("outbound>>>_access_peer>>>traffic>>>uplink", 1), stat("outbound>>>_access_peer>>>traffic>>>downlink", 2)},
		"inbound>>>": {
			stat("inbound>>>f1>>>traffic>>>uplink", 100),
			stat("inbound>>>f1>>>traffic>>>downlink", 200),
			stat("inbound>>>f2>>>traffic>>>uplink", 7),
			stat("inbound>>>f2>>>traffic>>>downlink", 8),
			stat("inbound>>>user-in>>>traffic>>>uplink", 500),
			stat("inbound>>>user-in>>>traffic>>>downlink", 500),
		},
	})
	accounts := map[string]traffic.Counter{
		"user>>>account":          {Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless"},
		"outbound>>>_access_peer": {Revision: 1, CredentialID: "peer", UserID: "user", DeviceID: "device", Protocol: "wireguard"},
	}
	adapter, batches := forwardStatsAdapter(forwardConfig(endpoint, accounts, "f1", "f2"))
	if err := adapter.SampleTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*batches) != 2 || len((*batches)[0]) != 2 {
		t.Fatal("accounts were not persisted as their own batch", *batches)
	}
	node := (*batches)[1]
	if len(node) != 1 || node[0].UploadBytes != 118 || node[0].DownloadBytes != 230 {
		t.Fatal("node total is not accounts plus forwards", node)
	}
	if patterns := fixture.queried(); len(patterns) != 3 || patterns[1] != "outbound>>>_access" || patterns[2] != "inbound>>>" {
		t.Fatal("unexpected statistics queries", patterns)
	}
}

func TestStatsReaderKeepsLazyForwardCountersUnobserved(t *testing.T) {
	_, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{"user>>>": {}, "inbound>>>": {}})
	adapter, batches := forwardStatsAdapter(forwardConfig(endpoint, map[string]traffic.Counter{}, "f", "g"))
	if err := adapter.SampleTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	node := (*batches)[1]
	if len(node) != 1 || node[0].UploadBytes != 0 || node[0].DownloadBytes != 0 {
		t.Fatal("lazy forward counters invented bytes", node)
	}
	status := adapter.TrafficSourceStatus()
	if status.Status != "partial" || status.ConfiguredSources != 2 || status.ObservedSources != 0 {
		t.Fatal("lazy forward counters were reported as measured", status)
	}
}

func TestStatsReaderRejectsInvalidForwardCounters(t *testing.T) {
	for name, values := range map[string][]*stats.Stat{
		"duplicate": {stat("inbound>>>f>>>traffic>>>uplink", 1), stat("inbound>>>f>>>traffic>>>uplink", 1)},
		"negative":  {stat("inbound>>>f>>>traffic>>>uplink", -1)},
		"one-sided": {stat("inbound>>>f>>>traffic>>>uplink", 1)},
		"direction": {stat("inbound>>>f>>>traffic>>>total", 1)},
		"overflow":  {stat("inbound>>>f1>>>traffic>>>uplink", math.MaxInt64), stat("inbound>>>f1>>>traffic>>>downlink", 0), stat("inbound>>>f2>>>traffic>>>uplink", 1), stat("inbound>>>f2>>>traffic>>>downlink", 0)},
	} {
		t.Run(name, func(t *testing.T) {
			_, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{"user>>>": {}, "inbound>>>": values})
			adapter, batches := forwardStatsAdapter(forwardConfig(endpoint, map[string]traffic.Counter{}, "f", "f1", "f2"))
			if err := adapter.SampleTraffic(context.Background()); err == nil {
				t.Fatal("invalid forward counters produced a sample")
			}
			if len(*batches) != 0 {
				t.Fatal("invalid forward counters reached the sink", *batches)
			}
		})
	}
	// A failed statistics query keeps the existing failure status and sample.
	fixture, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{"user>>>": {}, "inbound>>>": {}})
	fixture.mu.Lock()
	fixture.fail = true
	fixture.mu.Unlock()
	adapter, batches := forwardStatsAdapter(forwardConfig(endpoint, map[string]traffic.Counter{}, "f"))
	if err := adapter.SampleTraffic(context.Background()); err == nil || len(*batches) != 0 {
		t.Fatal("failed query produced a sample")
	}
	if status := adapter.TrafficSourceStatus(); status.Status != "sampling_failed" {
		t.Fatal("failed forward query was not reported", status)
	}
}

func TestStatsReaderFailsWhenAForwardSourceDisappears(t *testing.T) {
	fixture, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{"user>>>": {}})
	fixture.set("inbound>>>", stat("inbound>>>f>>>traffic>>>uplink", 4), stat("inbound>>>f>>>traffic>>>downlink", 5))
	adapter, batches := forwardStatsAdapter(forwardConfig(endpoint, map[string]traffic.Counter{}, "f"))
	if err := adapter.SampleTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.set("inbound>>>")
	if err := adapter.SampleTraffic(context.Background()); err == nil {
		t.Fatal("vanished forward source still sampled")
	}
	if len(*batches) != 2 {
		t.Fatal("failed sample persisted a node total", *batches)
	}
	if status := adapter.TrafficSourceStatus(); status.Status != "sampling_failed" || status.ConfiguredSources != 1 {
		t.Fatal("forward source failure was not reported", status)
	}
}

func TestStatsReaderReportsForwardSourceCoverage(t *testing.T) {
	fixture, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{
		"user>>>":    {stat("user>>>account>>>traffic>>>uplink", 1), stat("user>>>account>>>traffic>>>downlink", 2)},
		"inbound>>>": {stat("inbound>>>f>>>traffic>>>uplink", 3), stat("inbound>>>f>>>traffic>>>downlink", 4)},
	})
	accounts := map[string]traffic.Counter{"user>>>account": {Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless"}}
	adapter, _ := forwardStatsAdapter(forwardConfig(endpoint, accounts, "f", "g"))
	if err := adapter.SampleTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := adapter.TrafficSourceStatus(); status.Status != "partial" || status.ConfiguredSources != 3 || status.ObservedSources != 2 {
		t.Fatal("configured sources must count accounts and forwards only", status)
	}
	fixture.set("inbound>>>", stat("inbound>>>f>>>traffic>>>uplink", 3), stat("inbound>>>f>>>traffic>>>downlink", 4), stat("inbound>>>g>>>traffic>>>uplink", 5), stat("inbound>>>g>>>traffic>>>downlink", 6))
	if err := adapter.SampleTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := adapter.TrafficSourceStatus(); status.Status != "covered" || status.ObservedSources != 3 {
		t.Fatal("observed forward sources were not counted", status)
	}
}

func TestCollectStatsIncludesForwardTotals(t *testing.T) {
	_, endpoint := forwardStatsServer(t, map[string][]*stats.Stat{
		"user>>>":    {stat("user>>>account>>>traffic>>>uplink", 10), stat("user>>>account>>>traffic>>>downlink", 20)},
		"inbound>>>": {stat("inbound>>>f>>>traffic>>>uplink", 30), stat("inbound>>>f>>>traffic>>>downlink", 40)},
	})
	accounts := map[string]traffic.Counter{"user>>>account": {Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless"}}
	adapter, _ := forwardStatsAdapter(forwardConfig(endpoint, accounts, "f"))
	total, err := adapter.CollectStats(context.Background())
	if err != nil || total.Upload != 40 || total.Download != 60 {
		t.Fatal("live node total is not accounts plus forwards", total, err)
	}
	adapter.process = nil
	if _, err := adapter.CollectStats(context.Background()); err == nil {
		t.Fatal("stopped core reported a node total")
	}
}
