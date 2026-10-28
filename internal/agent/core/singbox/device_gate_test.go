package singbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeviceGateSingboxUserSourceJoinTTLAndProbeFailOpen(t *testing.T) {
	var mu sync.Mutex
	deleted := []string{}
	broken := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("missing API auth")
		}
		if r.Method == "DELETE" {
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(204)
			return
		}
		if broken {
			io.WriteString(w, `{"connections":[{"id":"private-source","metadata":{}}]}`)
			return
		}
		io.WriteString(w, `{"connections":[{"id":"first","metadata":{"sourceIP":"198.51.100.18","sourcePort":"12345"}},{"id":"second","metadata":{"sourceIP":"198.51.100.18","sourcePort":"12346"}}]}`)
	}))
	defer server.Close()
	cc := &collector.Context{Inbounds: map[string]collector.Inbound{"entry": {Protocol: "http", Accounts: map[string]collector.Account{"first": {ID: "c1"}, "second": {ID: "c2"}}}}}
	writer := collector.NewSingboxWriter(cc, nil, nil)
	for i, name := range []string{"first", "second"} {
		prefix := fmt.Sprintf("+0000 2026-10-27 12:00:00 INFO [%d 1ms] inbound/http[entry]: ", i+1)
		writer.Write([]byte(prefix + fmt.Sprintf("inbound connection from 198.51.100.18:%d\n", 12345+i)))
		writer.Write([]byte(prefix + "[" + name + "] inbound connection to example.com:443\n"))
	}
	now := time.Unix(1000, 0)
	a := &Adapter{Now: func() time.Time { return now }, process: &exec.Cmd{}, sourceWriter: writer, statsSecret: "fixture-secret", statsEpoch: "e", current: core.RenderedConfig{NodeID: "n", Revision: 1, StatsEndpoint: strings.TrimPrefix(server.URL, "http://"), AccessContext: cc}}
	ctx := context.Background()
	sample, err := a.ConnectionSnapshot(ctx)
	if err != nil || len(sample.Accounts) != 2 || sample.Connections != 2 {
		t.Fatal("observation failed", err)
	}
	command := []byte(`{"ttl_seconds":180,"entries":[{"credential_id":"c1","keys":["198.51.100.18"]}]}`)
	if err := a.SetDeviceGate(ctx, command); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(deleted) != 1 || deleted[0] != "/connections/first" {
		t.Error("gate affected wrong user")
	}
	mu.Unlock()
	now = now.Add(180 * time.Second)
	if err := a.TickDeviceGates(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(deleted) != 1 {
		t.Error("expired gate still closes connections")
	}
	broken = true
	mu.Unlock()
	err = a.SetDeviceGate(ctx, command)
	if err == nil || strings.Contains(err.Error(), "private-source") || len(a.deviceGate.Entries(now)) != 0 {
		t.Fatal("probe failure did not fail open safely")
	}
}
