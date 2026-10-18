package xray

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/internal/agent/reconcile"
	access "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealXrayAccessEventsActualRuleAndPrivateContentExcluded(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for core access event collection")
	}
	ctx := context.Background()
	events := make(chan access.Rollup, 32)
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: testNodeID, AccessSink: func(record access.Rollup) error { events <- record; return nil }}
	defer adapter.Close()
	state := testState(availablePort(t), 1)
	state.Policies = []desiredstate.Policy{{ID: "blocked-host", Enabled: true, Match: desiredstate.Match{Domains: []string{"blocked.example"}, Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}
	manager, err := reconcile.New(t.TempDir(), state.NodeID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "PRIVATE-RESPONSE-BODY") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(state.Inbounds[0].Port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request, _ := http.NewRequest("GET", origin.URL+"/PRIVATE-PATH?password=PRIVATE-QUERY", nil)
	request.Header.Set("Cookie", "PRIVATE-COOKIE")
	request.Header.Set("Authorization", "PRIVATE-AUTH")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response, err := client.Get("http://blocked.example/private"); err == nil {
		response.Body.Close()
		t.Fatal("blocked host passed")
	}
	seen := map[string]access.Rollup{}
	deadline := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case record := <-events:
			if record.Domain != "" {
				seen[record.Domain] = record
			} else {
				seen[record.TargetIP] = record
			}
		case <-deadline:
			t.Fatal("core access events missing", seen)
		}
	}
	allowed, ok := seen["127.0.0.1"]
	if !ok || !allowed.Allowed || allowed.Action != "Direct" || allowed.TrafficKnown || allowed.RuleID != "" {
		t.Fatal("actual direct record wrong", allowed)
	}
	blocked, ok := seen["blocked.example"]
	if !ok || blocked.Allowed || blocked.Action != "Block" || blocked.RuleID != "blocked-host" || blocked.OutboundID != "block" {
		t.Fatal("actual block/rule attribution wrong", blocked)
	}
	encoded, _ := json.Marshal(seen)
	if strings.Contains(string(encoded), "PRIVATE-") {
		t.Fatal("request path, header or response content escaped metadata extraction")
	}
}

// TestRealXrayAccessLogKeepsNoURLPathOnDisk proves the raw access log of a
// plain-HTTP proxy request leaves nothing on disk: the official core writes the
// whole URL, path and query included, into the Agent-owned file, so the Agent
// must empty that file once it has read it. The record handed to the collector
// still carries the authority only.
func TestRealXrayAccessLogKeepsNoURLPathOnDisk(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for core access event collection")
	}
	ctx := context.Background()
	events := make(chan access.Rollup, 32)
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: testNodeID, AccessSink: func(record access.Rollup) error { events <- record; return nil }}
	defer adapter.Close()
	state := testState(availablePort(t), 1)
	state.Inbounds[0].Protocol = "http"
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer origin.Close()
	parsedOrigin, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	originPort, err := strconv.Atoi(parsedOrigin.Port())
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(state.Inbounds[0].Port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get(origin.URL + "/AUDIT-PRIVATE-PATH?token=AUDIT-PRIVATE-QUERY")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	var record access.Rollup
	select {
	case record = <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("the core access event for the proxied request is missing")
	}
	if record.Domain != "" || record.TargetIP != "127.0.0.1" || record.TargetPort != originPort {
		t.Fatalf("the access record must carry the authority only: %+v", record)
	}
	// The raw line may exist only until the next poll read it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err := os.ReadFile(adapter.accessLogPath())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "AUDIT-PRIVATE-") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the on-disk access log kept the request path and query: %s", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
