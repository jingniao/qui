package xray

import (
	"context"
	"net"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/pkg/contracts/traffic"
	stats "proxycontrol/pkg/contracts/xraystatspb"
	"strconv"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This fixture exercises the real StatsService reader and bounded SQLite sink;
// it is not evidence of protocol forwarding by an installed Xray process.
type largeStatsFixture struct {
	stats.UnimplementedStatsServiceServer
	mu        sync.Mutex
	responses map[string][]*stats.Stat
	queries   int
}

func (s *largeStatsFixture) QueryStats(_ context.Context, request *stats.QueryStatsRequest) (*stats.QueryStatsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.Reset_ {
		return nil, status.Error(codes.InvalidArgument, "cumulative counters must never be reset")
	}
	values, known := s.responses[request.Pattern]
	if !known {
		return nil, status.Error(codes.InvalidArgument, "unexpected statistics query")
	}
	s.queries++
	return &stats.QueryStatsResponse{Stat: values}, nil
}

func TestNativeStatsReaderPersistsLargeMixedAccountSampleWithoutReset(t *testing.T) {
	ctx := context.Background()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &largeStatsFixture{responses: map[string][]*stats.Stat{"user>>>": {}, "outbound>>>_access": {}}}
	configuration := core.RenderedConfig{NodeID: "node", Revision: 1, StatsEndpoint: listener.Addr().String(), StatsAccounts: map[string]traffic.Counter{}}
	for index := 0; index < 600; index++ {
		pattern, prefix, protocol := "user>>>", "user>>>account-", "http"
		if index >= 300 {
			pattern, prefix, protocol = "outbound>>>_access", "outbound>>>_access_peer-", "wireguard"
		}
		name := prefix + strconv.Itoa(index)
		configuration.StatsAccounts[name] = traffic.Counter{Revision: 1, CredentialID: "credential-" + strconv.Itoa(index), UserID: "user", DeviceID: "device-" + strconv.Itoa(index), Protocol: protocol}
		fixture.responses[pattern] = append(fixture.responses[pattern], &stats.Stat{Name: name + ">>>traffic>>>uplink", Value: 1}, &stats.Stat{Name: name + ">>>traffic>>>downlink", Value: 1000})
	}
	fixture.responses["user>>>"] = append(fixture.responses["user>>>"], &stats.Stat{Name: "user>>>internal-relay>>>traffic>>>uplink", Value: 900000})
	server := grpc.NewServer()
	stats.RegisterStatsServiceServer(server, fixture)
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); listener.Close(); <-done })
	root := t.TempDir()
	queue, err := spool.OpenBound(ctx, filepath.Join(root, "spool"), "node", 1<<20, filepath.Join(root, "identity", "spool-store.json.enc"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	for attempt := 0; attempt < 2; attempt++ {
		values, err := readCounters(ctx, configuration, "native-epoch")
		if err != nil || len(values) != 600 {
			t.Fatal("large native sample lost accounts", len(values), err)
		}
		if err := queue.SaveTraffic(ctx, values); err != nil {
			t.Fatal("native sample could not persist", err)
		}
	}
	if queued, err := queue.Stats(ctx); err != nil || queued.Batches != 3 {
		t.Fatal("native repeated query did not produce exactly three immutable chunks", queued, err)
	}
	seen := map[string]bool{}
	var total int64
	for {
		batch, err := queue.PendingTraffic(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if batch == nil {
			break
		}
		if len(batch.Counters) > traffic.MaxBatchCounters {
			t.Fatal("native reader expanded wire batch limit")
		}
		for _, counter := range batch.Counters {
			if seen[counter.CredentialID] || counter.UploadBytes != 1 || counter.DownloadBytes != 1000 || counter.CoreEpoch != "native-epoch" {
				t.Fatal("native account was duplicated or incorrectly combined", counter)
			}
			seen[counter.CredentialID] = true
			total += counter.UploadBytes + counter.DownloadBytes
		}
		if err := queue.AckTraffic(ctx, batch.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 600 || total != 600600 {
		t.Fatal("native statistics chunks lost or invented usage", len(seen), total)
	}
	fixture.mu.Lock()
	fixture.responses["outbound>>>_access"] = append(fixture.responses["outbound>>>_access"], &stats.Stat{Name: "outbound>>>_access_peer-599>>>traffic>>>uplink", Value: -1})
	queries := fixture.queries
	fixture.mu.Unlock()
	if queries != 4 {
		t.Fatal("unexpected native query count", queries)
	}
	if values, err := readCounters(ctx, configuration, "native-epoch"); err == nil || len(values) != 0 {
		t.Fatal("invalid late statistic returned partial valid sample", len(values), err)
	}
	if queued, err := queue.Stats(ctx); err != nil || queued.Batches != 0 {
		t.Fatal("native read failure recreated acknowledged queue data", queued, err)
	}
}
