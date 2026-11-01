package xray

import (
	"context"
	"net"
	"os"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"strconv"
	"testing"
	"time"
)

// TestRealOnlineStatisticsRetainASourceBetweenSamples proves the observation
// retention against the official core: upstream lists a client address only
// around the life of its connections, so a source that connected and closed
// between two samples must still reach the snapshot the heartbeat reports, and a
// later sample without it must not take it away.
//
// Upstream ignores loopback client addresses (app/stats/online_map.go), so the
// test needs a non-loopback address and skips without one. A single-homed host
// offers exactly one such source, which is what both accounts below use.
func TestRealOnlineStatisticsRetainASourceBetweenSamples(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for upstream online statistics")
	}
	address := nonLoopbackIPv4()
	if address == "" {
		t.Skip("upstream excludes loopback clients from online statistics")
	}
	key, ok := traffic.AddressKey(address)
	if !ok {
		t.Fatal("host address is not observable", address)
	}
	ctx := context.Background()
	state := testState(availablePort(t), 1)
	state.Inbounds[0].Protocol = "http"
	state.Users = []desiredstate.User{{ID: "holding-user", Enabled: true}, {ID: "short-user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{
		{ID: "holding", UserID: "holding-user", DeviceID: "holding", Protocol: "http", Username: "holding", Secret: "holding-password"},
		{ID: "short", UserID: "short-user", DeviceID: "short", Protocol: "http", Username: "short", Secret: "short-password"},
	}
	state.Inbounds[0].CredentialIDs = []string{"holding", "short"}
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
	origin := testutil.GateEcho(t)
	proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(state.Inbounds[0].Port))
	source := net.ParseIP(address)
	// The holding account keeps the source alive for the whole run, so only the
	// short account's entry can disappear from the core between samples.
	holding, err := testutil.GateTunnel(proxy, origin, "holding", "holding-password", source)
	if err != nil {
		t.Fatal(err)
	}
	defer holding.Close()
	if !testutil.GateRoundTrip(holding) {
		t.Fatal("baseline traffic failed")
	}
	observe := func() traffic.ConnectionSnapshot {
		adapter.SampleConnections(ctx)
		snapshot, err := adapter.ObservedConnections(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	keys := func(snapshot traffic.ConnectionSnapshot, credential string) []string {
		for _, account := range snapshot.Accounts {
			if account.CredentialID == credential {
				return account.AddressKeys
			}
		}
		return nil
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(keys(observe(), "holding")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the open connection was not observed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	short, err := testutil.GateTunnel(proxy, origin, "short", "short-password", source)
	if err != nil {
		t.Fatal(err)
	}
	short.Close()
	// The core still lists the address for a moment after the close; the sample
	// taken here is what the retention window starts from.
	if got := keys(observe(), "short"); len(got) != 1 || got[0] != key {
		t.Fatalf("the short connection was not caught: %v", got)
	}
	deadline = time.Now().Add(8 * time.Second)
	for {
		observed, err := adapter.ConnectionSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(keys(observed, "short")) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the core kept the closed source")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := keys(observe(), "short"); len(got) != 1 || got[0] != key {
		t.Fatalf("a later sample without the source dropped it from the snapshot: %v", got)
	}
}
