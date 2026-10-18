package xray

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	access "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func TestRealAccessIdentitiesAcrossTwoDevicesAndProtocols(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for protocol telemetry identity")
	}
	for _, protocol := range []string{"http", "socks5", "vless", "trojan", "shadowsocks"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			first := proxyCredentials(t, protocol)
			first.ID = "first-credential"
			first.UserID = "user"
			first.DeviceID = "first"
			first.Username = "first"
			first.Internal = false
			second := proxyCredentials(t, protocol)
			second.ID = "second-credential"
			second.UserID = "user"
			second.DeviceID = "second"
			second.Username = "second"
			second.Internal = false
			serverPort := availablePort(t)
			state := testState(serverPort, 1)
			state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
			state.Credentials = []desiredstate.Credential{first, second}
			state.Inbounds[0].Protocol = protocol
			state.Inbounds[0].CredentialIDs = []string{first.ID, second.ID}
			settings := desiredstate.ProtocolSettings{}
			if protocol == "trojan" {
				settings = tlsMaterials(t)
				state.Inbounds[0].Settings = settings
			}
			state.Policies = []desiredstate.Policy{{ID: "first-block", Enabled: true, Scope: desiredstate.Scope{DeviceIDs: []string{"first"}}, Match: desiredstate.Match{Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}
			events := make(chan access.Rollup, 16)
			adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID, EnableStats: true, AccessSink: func(record access.Rollup) error { events <- record; return nil }}
			defer adapter.Close()
			rendered, err := adapter.Render(ctx, state)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "protocol-audit-origin") }))
			defer origin.Close()
			for index, credential := range []desiredstate.Credential{first, second} {
				localPort := availablePort(t)
				client := testState(localPort, 1)
				client.Users = []desiredstate.User{{ID: "user", Enabled: true}}
				client.Credentials = []desiredstate.Credential{credential}
				client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "remote", Protocol: protocol, Address: "127.0.0.1", Port: serverPort, Enabled: true, CredentialID: credential.ID, Settings: clientTLS(settings)})
				client.Inbounds[0].DefaultOutbound = "remote"
				startState(t, client)
				_, allowed := domainRequest(t, localPort, "127.0.0.1", origin.Listener.Addr().(*net.TCPAddr).Port)
				if allowed != (index == 1) {
					t.Fatal("instrumentation changed actual forwarding", protocol, index)
				}
			}
			seen := map[string]access.Rollup{}
			deadline := time.After(5 * time.Second)
			for len(seen) < 2 {
				select {
				case record := <-events:
					seen[record.DeviceID] = record
				case <-deadline:
					t.Fatal("device audit metadata missing", protocol, seen)
				}
			}
			if seen["first"].UserID != "user" || seen["first"].Allowed || seen["first"].RuleID != "first-block" || seen["second"].UserID != "user" || !seen["second"].Allowed {
				t.Fatal("cross-device attribution wrong", seen)
			}
			counters, err := adapter.TrafficCounters(ctx)
			if err != nil {
				t.Fatal(err)
			}
			measuredSecond := false
			for _, counter := range counters {
				if counter.DeviceID == "second" && counter.UserID == "user" && counter.DownloadBytes > 0 {
					measuredSecond = true
				}
			}
			if !measuredSecond {
				t.Fatal("native account byte measurement missing for allowed device", protocol, counters)
			}
		})
	}
}
