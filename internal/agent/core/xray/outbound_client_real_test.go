package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

// outboundClientSettings derives the outbound client half from a listener's
// settings, the way the panel converts an imported endpoint (v1.6.0).
func outboundClientSettings(server desiredstate.ProtocolSettings) desiredstate.ProtocolSettings {
	client := desiredstate.ProtocolSettings{Security: server.Security, Transport: server.Transport, Flow: server.Flow, ServiceName: server.ServiceName, ClientFingerprint: "chrome"}
	if server.XHTTP != nil {
		x := *server.XHTTP
		client.XHTTP = &x
	}
	if r := server.Reality; r != nil {
		client.ServerName = r.ServerNames[0]
		client.RealityClient = &desiredstate.RealityClientSettings{PublicKey: r.PublicKey, ShortID: r.ShortIDs[0], SpiderX: "/"}
	} else {
		client.ServerName, client.TrustedCAPEM, client.ALPN = "localhost", server.TrustedCAPEM, []string{"h2"}
	}
	return client
}

// startRenderedClient applies a client Agent whose SOCKS5 listener routes to one
// VLESS outbound rendered by this package from the v1.6.0 client fields.
func startRenderedClient(t *testing.T, ctx context.Context, settings desiredstate.ProtocolSettings, serverPort int, secret string) (*Adapter, int) {
	t.Helper()
	port := availablePort(t)
	state := testState(port, 1)
	state.Inbounds[0].DefaultOutbound = "upstream"
	state.Credentials = []desiredstate.Credential{{ID: "upstream-credential", Protocol: "vless", Secret: secret}}
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: "upstream", Protocol: "vless", Address: "127.0.0.1", Port: serverPort, Enabled: true, CredentialID: "upstream-credential", Settings: settings, SupportTCP: true})
	a := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: testNodeID}
	t.Cleanup(func() { a.Close() })
	rendered, err := a.Render(ctx, state)
	if err != nil {
		t.Fatal("client render failed", err)
	}
	if result, err := a.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal("client native configuration/application failed", err)
	}
	return a, port
}

func TestRealRenderedOutboundClientReachesSelfHostedServers(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("XRAY_BINARY is required for v1.6.0 outbound client handshakes")
	}
	ctx := context.Background()
	camouflage := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	camouflage.EnableHTTP2 = true
	camouflage.Config.ErrorLog = log.New(io.Discard, "", 0)
	camouflage.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	camouflage.StartTLS()
	defer camouflage.Close()
	for _, tc := range []struct{ name, security, transport, flow string }{
		{"reality-tcp", "reality", "tcp", ""}, {"reality-vision", "reality", "tcp", "xtls-rprx-vision"}, {"reality-grpc", "reality", "grpc", ""},
		{"reality-xhttp", "reality", "xhttp", ""}, {"tls-xhttp", "tls", "xhttp", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := realityTestSettings(t, camouflage.Listener.Addr().String())
			if tc.security == "tls" {
				settings = tlsMaterials(t)
			}
			settings.Transport, settings.Flow = tc.transport, tc.flow
			if tc.transport == "grpc" {
				settings.ServiceName = "outbound-grpc"
			}
			if tc.transport == "xhttp" {
				settings.XHTTP = &desiredstate.XHTTPSettings{Host: "localhost", Path: "/outbound-probe", Mode: "auto"}
			}
			server := testState(availablePort(t), 1)
			server.Inbounds[0].Listen = "0.0.0.0"
			server.Inbounds[0].ID, server.Inbounds[0].Protocol, server.Inbounds[0].Settings = "vless-entry", "vless", settings
			server.Users = []desiredstate.User{{ID: "user", Enabled: true}}
			server.Credentials = []desiredstate.Credential{{ID: "account", UserID: "user", DeviceID: "identity", Protocol: "vless", Secret: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}}
			server.Inbounds[0].CredentialIDs = []string{"account"}
			a := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: testNodeID}
			defer a.Close()
			rendered, err := a.Render(ctx, server)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := a.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatal("server native/application failed", err)
			}
			var hits atomic.Int32
			nonce := strings.Repeat("outbound-"+tc.name+"-", 800)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); io.WriteString(w, nonce) }))
			defer origin.Close()
			client, port := startRenderedClient(t, ctx, outboundClientSettings(settings), server.Inbounds[0].Port, server.Credentials[0].Secret)
			socksRequestUntil(t, port, origin.URL, nonce)
			if hits.Load() < 1 {
				t.Fatal("origin did not receive the request relayed by the rendered outbound")
			}
			client.Close()
			if tc.security != "reality" || tc.transport != "tcp" || tc.flow != "" {
				return
			}
			// A client holding another public key must not authenticate.
			wrong := outboundClientSettings(settings)
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			wrong.RealityClient.PublicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
			before := hits.Load()
			_, wrongPort := startRenderedClient(t, ctx, wrong, server.Inbounds[0].Port, server.Credentials[0].Secret)
			if response, err := socksExchange(wrongPort, origin.URL); err == nil && strings.Contains(response, nonce) {
				t.Fatal("a wrong Reality public key reached the origin")
			}
			if hits.Load() != before {
				t.Fatal("a wrong Reality public key reached the origin")
			}
		})
	}
}
