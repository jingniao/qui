package singbox

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRealRealityTCPAuthenticatedHTTPAndRollback(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for native Reality acceptance")
	}
	ctx := context.Background()
	// Both handshake and origin stay on loopback; no external site or DNS.
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	target.EnableHTTP2 = true
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	target.StartTLS()
	defer target.Close()
	for _, flow := range []string{"", "xtls-rprx-vision"} {
		name := flow
		if name == "" {
			name = "no-flow"
		}
		t.Run(name, func(t *testing.T) {
			state := realityState(t, flow, freePort(t), target.Listener.Addr().String())
			server := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
			defer server.Close()
			server.ConfigureRuntime(core.RuntimeHooks{EnableStats: true})
			caps, err := server.DetectCapabilities(ctx)
			if err != nil {
				t.Fatal(err)
			}
			capability := server.nativeServerSettings
			if err := desiredstate.RequireInboundServerSettings("sing-box", capability, "vless", state.Inbounds[0].Settings); err != nil {
				t.Fatalf("native probe did not authorize test combination: %v", err)
			}
			for _, combination := range caps.ServerSettingsCombinations {
				if combination.Protocol != "vless" || combination.Direction != "inbound" || combination.Security != "reality" || combination.Transport != "tcp" {
					t.Fatal("native report advertised an unverified combination")
				}
			}
			rendered, err := server.Render(ctx, state)
			if err != nil {
				t.Fatal(err)
			}
			if err := server.Validate(ctx, rendered); err != nil {
				t.Fatal(err)
			}
			if result, err := server.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatalf("server apply: %v, %v", result.Code, err)
			}
			var hits atomic.Int64
			nonceBytes := make([]byte, 24)
			if _, err := rand.Read(nonceBytes); err != nil {
				t.Fatal(err)
			}
			nonce := hex.EncodeToString(nonceBytes)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/"+nonce {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				hits.Add(1)
				io.WriteString(w, nonce)
			}))
			defer origin.Close()
			startClient := func(uuid, shortID string) (*Adapter, string) {
				t.Helper()
				port := freePort(t)
				endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
				// Public client fixture only. M2 owns the shared subscription mapper.
				doc := map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{map[string]any{"type": "http", "tag": "local", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []any{map[string]any{
					"type": "vless", "tag": "remote", "server": "127.0.0.1", "server_port": state.Inbounds[0].Port, "uuid": uuid, "flow": flow,
					"tls": map[string]any{"enabled": true, "server_name": "localhost", "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": state.Inbounds[0].Settings.Reality.PublicKey, "short_id": shortID}},
				}}, "route": map[string]any{"final": "remote"}}
				data, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				client := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: "reality-client"}
				t.Cleanup(func() { client.Close() })
				config := core.RenderedConfig{NodeID: "reality-client", Revision: 1, Data: data, HealthEndpoints: []string{endpoint}}
				if err := client.Validate(ctx, config); err != nil {
					t.Fatal("native client validation failed", err)
				}
				if result, err := client.Apply(ctx, config); err != nil || !result.Success {
					t.Fatal("client apply failed", err)
				}
				return client, endpoint
			}
			request := func(endpoint string) error {
				proxy, _ := url.Parse("http://" + endpoint)
				transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
				defer transport.CloseIdleConnections()
				response, err := (&http.Client{Transport: transport, Timeout: 2 * time.Second}).Get(origin.URL + "/" + nonce)
				if err != nil {
					return err
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err == nil && (response.StatusCode != http.StatusOK || string(body) != nonce) {
					return errors.New("authenticated nonce missing")
				}
				return err
			}
			for index, shortID := range state.Inbounds[0].Settings.Reality.ShortIDs {
				client, endpoint := startClient(state.Credentials[0].Secret, shortID)
				if err := request(endpoint); err != nil {
					t.Fatalf("authorized ShortID %d nonce failed: %v", index, err)
				}
				client.Close()
			}
			before := hits.Load()
			badClient, badEndpoint := startClient("7ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", state.Inbounds[0].Settings.Reality.ShortIDs[0])
			if err := request(badEndpoint); err == nil || hits.Load() != before {
				t.Fatal("wrong VLESS credential reached origin")
			}
			badClient.Close()
			if !caps.SingboxUserStats {
				if server.SupportsAccountTelemetry() {
					t.Fatal("official core invented account statistics")
				}
				if _, err := server.TrafficCounters(ctx); !errors.Is(err, ErrTelemetryUnavailable) {
					t.Fatal("official per-user traffic must remain unknown", err)
				}
			} else {
				counters, err := server.TrafficCounters(ctx)
				if err != nil || len(counters) == 0 {
					t.Fatal("with_v2ray_api counters unavailable", err)
				}
				measured := false
				for _, counter := range counters {
					if counter.CredentialID == state.Credentials[0].ID && counter.UploadBytes+counter.DownloadBytes > 0 {
						measured = true
					}
				}
				if !measured {
					t.Fatal("Reality user traffic not measured by stats-enabled core")
				}
			}
			// A failed startup with a new key must restore the previous key too.
			conflict, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			failed := realityState(t, flow, conflict.Addr().(*net.TCPAddr).Port, target.Listener.Addr().String())
			failed.Revision = 2
			failedRendered, err := server.Render(ctx, failed)
			if err != nil {
				conflict.Close()
				t.Fatal(err)
			}
			result, applyErr := server.Apply(ctx, failedRendered)
			conflict.Close()
			if applyErr == nil || !result.RolledBack {
				t.Fatalf("failed native apply did not restore Reality revision: %s", result.Code)
			}
			client, endpoint := startClient(state.Credentials[0].Secret, state.Inbounds[0].Settings.Reality.ShortIDs[0])
			if err := request(endpoint); err != nil {
				t.Fatal("failed apply lost original Reality keys", err)
			}
			client.Close()
			// A new draft key must not mutate the successfully applied revision.
			next := realityState(t, flow, state.Inbounds[0].Port, target.Listener.Addr().String())
			next.Revision = 3
			nextRendered, err := server.Render(ctx, next)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := server.Apply(ctx, nextRendered); err != nil || !result.Success {
				t.Fatal("rotated key apply failed", err)
			}
			if err := server.Rollback(ctx, 1); err != nil {
				t.Fatal("historical key rollback failed", err)
			}
			client, endpoint = startClient(state.Credentials[0].Secret, state.Inbounds[0].Settings.Reality.ShortIDs[0])
			if err := request(endpoint); err != nil {
				t.Fatal("rollback lost original Reality keys", err)
			}
			client.Close()
			if hits.Load() != before+2 {
				t.Fatal("unexpected origin hit count")
			}
			t.Logf("native+nonce+two ShortIDs+wrong credential+failed apply+key rotation/rollback PASS; userStats=%v; originHits=%d", caps.SingboxUserStats, hits.Load())
		})
	}
}

func TestRealSingboxXHTTPRemainsUnavailable(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for native xhttp negative")
	}
	// This direct native fixture confirms the pinned core's rejection separately
	// from the Agent's earlier typed refusal. No listener is started.
	data := []byte(`{"inbounds":[{"type":"vless","listen":"127.0.0.1","listen_port":24443,"users":[{"uuid":"5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}],"transport":{"type":"xhttp","path":"/"}}],"outbounds":[{"type":"direct"}]}`)
	a := &Adapter{Path: binary, Directory: t.TempDir()}
	if err := a.Validate(context.Background(), core.RenderedConfig{Data: data}); err == nil {
		t.Fatal("core unexpectedly accepts xhttp; evidence/model must be reviewed before enabling")
	}
}
