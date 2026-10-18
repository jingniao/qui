package xray

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealCumulativeUserCountersAndManagementPortIsolation(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for accurate native user counters")
	}
	ctx := context.Background()
	state := testState(availablePort(t), 1)
	state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	state.Credentials = []desiredstate.Credential{{ID: "device-credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "device-account", Secret: "stats-test-password"}}
	state.Inbounds[0].CredentialIDs = []string{"device-credential"}
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID, EnableStats: true}
	defer adapter.Close()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("counter-data", 1000)) }))
	defer origin.Close()
	proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(state.Inbounds[0].Port))
	proxyURL.User = url.UserPassword("device-account", "stats-test-password")
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for index := 0; index < 2; index++ {
		response, err := client.Get(origin.URL)
		if err != nil {
			t.Fatal("statistics instrumentation broke forwarding")
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	counters, err := adapter.TrafficCounters(ctx)
	if err != nil || len(counters) != 1 || counters[0].UserID != "user" || counters[0].DeviceID != "device" || counters[0].UploadBytes <= 0 || counters[0].DownloadBytes < 24000 {
		t.Fatal("native counters missing", counters, err)
	}
	second, err := adapter.TrafficCounters(ctx)
	if err != nil || second[0].DownloadBytes != counters[0].DownloadBytes || second[0].CoreEpoch != counters[0].CoreEpoch {
		t.Fatal("read reset cumulative counters", second, err)
	}
	// Arbitrary client hostname cannot bypass the reserved management port guard.
	if response, err := client.Get("http://localhost:" + strconv.Itoa(adapter.statsPort)); err == nil {
		response.Body.Close()
		t.Fatal("proxy client reached statistics API")
	}
	oldEpoch := counters[0].CoreEpoch
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal("restarted proxy failed")
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	restarted, err := adapter.TrafficCounters(ctx)
	if err != nil || len(restarted) != 1 || restarted[0].CoreEpoch == oldEpoch || restarted[0].DownloadBytes >= counters[0].DownloadBytes {
		t.Fatal("new process epoch not separated", restarted, err)
	}
}

func TestRealFailedFinalTrafficPersistenceKeepsCoreAndExplicitCloseStopsIt(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for traffic storage fault")
	}
	ctx := context.Background()
	state := testState(availablePort(t), 1)
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID, EnableStats: true}
	defer adapter.Close()
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	// Only the read-only service is enabled; mutable management is absent.
	var configured configDocument
	if err := json.Unmarshal(rendered.Data, &configured); err != nil {
		t.Fatal(err)
	}
	if configured.API == nil || len(configured.API.Services) != 1 || configured.API.Services[0] != "StatsService" {
		t.Fatal("statistics enabled a mutable management service")
	}
	adapter.TrafficSink = func(context.Context, []traffic.Counter) error { return errors.New("disk full") }
	next := state
	next.Revision = 2
	rendered, err = adapter.Render(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Apply(ctx, rendered)
	if err == nil || result.Code != "stop_failed" {
		t.Fatal("configuration switched despite unsaved traffic", result, err)
	}
	if health, err := adapter.Health(ctx); err != nil || !health.Healthy {
		t.Fatal("storage fault stopped prior proxy")
	}
	if err := adapter.Close(); err == nil {
		t.Fatal("failed shutdown sample not reported")
	}
	if adapter.process != nil {
		t.Fatal("explicit shutdown orphaned core")
	}
}

func TestStatsGuardIncludesHiddenProbeInbounds(t *testing.T) {
	state := testState(1080, 1)
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: "proxy", Protocol: "http", Address: "192.0.2.1", Port: 8080, Enabled: true})
	state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "pool", Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: "proxy", Weight: 1}}, ProbeURL: "https://probe.example", ProbeInterval: 30, Failover: "Block"}}
	rendered, err := renderWithOptions(context.Background(), state, time.Now(), RenderOptions{StatsPort: 21000, ProbePorts: map[string]int{"proxy": 21001}})
	if err != nil {
		t.Fatal(err)
	}
	var document configDocument
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
	guard := document.Routing.Rules[0]
	if guard.Port != "21000" || !inList(guard.InboundTag, "_probe_proxy") || !inList(guard.InboundTag, state.Inbounds[0].ID) {
		t.Fatal("probe inbound can bypass management isolation", guard)
	}
}
