package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

func realityTestSettings(t *testing.T, target string) desiredstate.ProtocolSettings {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return desiredstate.ProtocolSettings{Security: "reality", Transport: "tcp", Reality: &desiredstate.RealityServerSettings{PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), HandshakeTarget: target, ServerNames: []string{"localhost"}, ShortIDs: []string{"0123abcd"}}}
}

func startRealityTestClient(t *testing.T, settings desiredstate.ProtocolSettings, serverPort int, secret string) (*Adapter, int) {
	t.Helper()
	port := availablePort(t)
	stream := map[string]any{"network": settings.Transport, "security": settings.Security}
	if r := settings.Reality; r != nil {
		stream["realitySettings"] = map[string]any{"serverName": r.ServerNames[0], "publicKey": r.PublicKey, "shortId": r.ShortIDs[0], "fingerprint": "chrome"}
	}
	if settings.Security == "tls" {
		stream["tlsSettings"] = map[string]any{"serverName": "localhost", "alpn": []string{"h2"}, "disableSystemRoot": true, "certificates": []map[string]any{{"certificate": strings.Split(strings.TrimSpace(settings.TrustedCAPEM), "\n"), "usage": "verify"}}}
	}
	if settings.XHTTP != nil {
		stream["xhttpSettings"] = settings.XHTTP
	}
	if settings.Transport == "grpc" {
		stream["grpcSettings"] = map[string]string{"serviceName": settings.ServiceName}
	}
	data, err := json.Marshal(map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "sendThrough": nonLoopbackIPv4(), "settings": map[string]any{"vnext": []any{map[string]any{"address": nonLoopbackIPv4(), "port": serverPort, "users": []any{map[string]any{"id": secret, "encryption": "none", "flow": settings.Flow}}}}}, "streamSettings": stream}}})
	if err != nil {
		t.Fatal(err)
	}
	a := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: testNodeID}
	if result, err := a.Apply(context.Background(), core.RenderedConfig{NodeID: testNodeID, Revision: 1, Data: data, HealthEndpoints: []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}); err != nil || !result.Success {
		t.Fatal("client native configuration/application failed", err)
	}
	t.Cleanup(func() { a.Close() })
	return a, port
}

func TestRealSelfHostedRealityXHTTPAuthenticationTrafficGateAndRollback(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("XRAY_BINARY is required for M2 real handshakes")
	}
	ctx := context.Background()
	camouflage := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	camouflage.EnableHTTP2 = true
	camouflage.Config.ErrorLog = log.New(io.Discard, "", 0)
	camouflage.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	camouflage.StartTLS()
	defer camouflage.Close()
	for _, tc := range []struct{ name, security, transport, flow, mode string }{
		{"reality-tcp", "reality", "tcp", "", ""}, {"reality-vision", "reality", "tcp", "xtls-rprx-vision", ""}, {"reality-grpc", "reality", "grpc", "", ""},
		{"reality-xhttp-auto", "reality", "xhttp", "", "auto"}, {"reality-xhttp-packet-up", "reality", "xhttp", "", "packet-up"}, {"reality-xhttp-stream-up", "reality", "xhttp", "", "stream-up"}, {"reality-xhttp-stream-one", "reality", "xhttp", "", "stream-one"},
		{"tls-xhttp-auto", "tls", "xhttp", "", "auto"},
		{"tls-xhttp-packet-up", "tls", "xhttp", "", "packet-up"},
		{"tls-xhttp-stream-up", "tls", "xhttp", "", "stream-up"},
		{"tls-xhttp-stream-one", "tls", "xhttp", "", "stream-one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := realityTestSettings(t, camouflage.Listener.Addr().String())
			if tc.security == "tls" {
				settings = tlsMaterials(t)
			}
			settings.Transport, settings.Flow = tc.transport, tc.flow
			if tc.transport == "grpc" {
				settings.ServiceName = "s0 grpc"
			}
			if tc.transport == "xhttp" {
				settings.XHTTP = &desiredstate.XHTTPSettings{Host: "localhost", Path: "/m2-probe", Mode: tc.mode}
			}
			state := testState(availablePort(t), 1)
			state.Inbounds[0].Listen = "0.0.0.0"
			state.Inbounds[0].ID, state.Inbounds[0].Protocol, state.Inbounds[0].Settings = "vless-entry", "vless", settings
			state.Users = []desiredstate.User{{ID: "user", Enabled: true}}
			state.Credentials = []desiredstate.Credential{{ID: "account", UserID: "user", DeviceID: "identity", Protocol: "vless", Secret: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}}
			state.Inbounds[0].CredentialIDs = []string{"account"}
			a := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: testNodeID, EnableStats: true}
			defer a.Close()
			rendered, err := a.Render(ctx, state)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := a.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatal("server native/application failed", err)
			}
			var hits atomic.Int32
			nonce := strings.Repeat("authenticated-"+tc.name+"-", 1600)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); io.WriteString(w, nonce) }))
			defer origin.Close()
			client, port := startRealityTestClient(t, settings, state.Inbounds[0].Port, state.Credentials[0].Secret)
			socksRequestUntil(t, port, origin.URL, nonce)
			if hits.Load() < 1 {
				t.Fatal("origin did not receive authenticated request")
			}
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				counters, err := a.TrafficCounters(ctx)
				if err == nil && len(counters) == 1 && counters[0].UserID == "user" && counters[0].DownloadBytes > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("new transport did not attribute user traffic")
				}
				time.Sleep(25 * time.Millisecond)
			}
			// Malformed native settings do not replace the currently serving key.
			bad := rendered
			bad.Revision = 2
			bad.Data = []byte(`{"inbounds":[{"protocol":"missing"}]}`)
			if result, err := a.Apply(ctx, bad); err == nil || result.Success {
				t.Fatal("native failure reported success")
			}
			if health, err := a.Health(ctx); err != nil || !health.Healthy {
				t.Fatal("native failure damaged successful state")
			}
			if tc.name == "reality-tcp" || tc.name == "reality-xhttp-auto" {
				rotatedSettings := settings
				rotatedSettings.Reality = realityTestSettings(t, camouflage.Listener.Addr().String()).Reality
				rotatedState := state
				rotatedState.Revision = 2
				rotatedState.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
				rotatedState.Inbounds[0].Settings = rotatedSettings
				rotatedConfig, err := a.Render(ctx, rotatedState)
				if err != nil {
					t.Fatal(err)
				}
				if result, err := a.Apply(ctx, rotatedConfig); err != nil || !result.Success {
					t.Fatal("rotation failed", err)
				}
				rotatedClient, rotatedPort := startRealityTestClient(t, rotatedSettings, state.Inbounds[0].Port, state.Credentials[0].Secret)
				socksRequestUntil(t, rotatedPort, origin.URL, nonce)
				rotatedClient.Close()
				if err := a.Rollback(ctx, 1); err != nil {
					t.Fatal("historical key rollback failed", err)
				}
				restoredClient, restoredPort := startRealityTestClient(t, settings, state.Inbounds[0].Port, state.Credentials[0].Secret)
				socksRequestUntil(t, restoredPort, origin.URL, nonce)
				restoredClient.Close()
				source := nonLoopbackIPv4()
				if source == "" {
					t.Fatal("gate acceptance requires a non-loopback interface")
				}
				gate, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []map[string]any{{"credential_id": "account", "keys": []string{source}}}})
				if err := a.SetDeviceGate(ctx, gate); err != nil {
					t.Fatal(err)
				}
				before := hits.Load()
				blocked, blockedPort := startRealityTestClient(t, settings, state.Inbounds[0].Port, state.Credentials[0].Secret)
				response, err := socksExchange(blockedPort, origin.URL)
				blocked.Close()
				if err == nil && strings.Contains(response, nonce) || hits.Load() != before {
					t.Fatal("device gate did not reject new user connection")
				}
				clearGate, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []any{}})
				if err := a.SetDeviceGate(ctx, clearGate); err != nil {
					t.Fatal(err)
				}
				invalid, invalidPort := startRealityTestClient(t, settings, state.Inbounds[0].Port, "d6a5a416-94ef-4ad7-925a-aedba564bf12")
				response, err = socksExchange(invalidPort, origin.URL)
				invalid.Close()
				if err == nil && strings.Contains(response, nonce) || hits.Load() != before {
					t.Fatal("unpublished user credential authenticated")
				}
			}
		})
	}
}
