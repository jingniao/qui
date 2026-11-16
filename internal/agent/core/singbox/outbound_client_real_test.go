package singbox

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// TestRealRenderedRealityOutboundAuthenticates dials a sing-box Reality listener
// through an outbound this package renders from the v1.6.0 client fields.
func TestRealRenderedRealityOutboundAuthenticates(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for native Reality outbound acceptance")
	}
	ctx := context.Background()
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	target.EnableHTTP2 = true
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	target.StartTLS()
	defer target.Close()
	var hits atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); io.WriteString(w, "outbound-ok") }))
	defer origin.Close()
	for _, flow := range []string{"", "xtls-rprx-vision"} {
		t.Run("flow="+flow, func(t *testing.T) {
			state := realityState(t, flow, freePort(t), target.Listener.Addr().String())
			server := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
			defer server.Close()
			rendered, err := server.Render(ctx, state)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := server.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatalf("server apply: %v, %v", result.Code, err)
			}
			reality := state.Inbounds[0].Settings.Reality
			client := desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Flow: flow, ServerName: reality.ServerNames[0], ClientFingerprint: "chrome",
				RealityClient: &desiredstate.RealityClientSettings{PublicKey: reality.PublicKey, ShortID: reality.ShortIDs[0]}}
			request := func(settings desiredstate.ProtocolSettings) error {
				port := freePort(t)
				clientState := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "outbound-client", Logging: desiredstate.Logging{DomainOnly: true},
					Users:       []desiredstate.User{{ID: "client-user", Enabled: true}},
					Credentials: []desiredstate.Credential{{ID: "local-credential", UserID: "client-user", DeviceID: "client-device", Protocol: "http", Username: "client", Secret: "client-secret"}, {ID: "upstream-credential", Protocol: "vless", Secret: state.Credentials[0].Secret}},
					Inbounds:    []desiredstate.Inbound{{ID: "local", Core: "sing-box", Protocol: "http", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "upstream", CredentialIDs: []string{"local-credential"}}},
					Outbounds:   []desiredstate.Outbound{{ID: "upstream", Protocol: "vless", Address: "127.0.0.1", Port: state.Inbounds[0].Port, Enabled: true, CredentialID: "upstream-credential", Settings: settings, SupportTCP: true}, {ID: "direct", Protocol: "direct", Enabled: true}}}
				adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: clientState.NodeID}
				defer adapter.Close()
				config, err := adapter.Render(ctx, clientState)
				if err != nil {
					t.Fatal("client render failed", err)
				}
				if result, err := adapter.Apply(ctx, config); err != nil || !result.Success {
					t.Fatal("client apply failed", err)
				}
				proxy, _ := url.Parse("http://client:client-secret@127.0.0.1:" + strconv.Itoa(port))
				transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
				defer transport.CloseIdleConnections()
				response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
				if err != nil {
					return err
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err == nil && string(body) != "outbound-ok" {
					return errors.New("origin answer missing")
				}
				return err
			}
			if err := request(client); err != nil {
				t.Fatalf("rendered Reality outbound failed: %v", err)
			}
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			wrong := client
			wrong.RealityClient = &desiredstate.RealityClientSettings{PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: reality.ShortIDs[0]}
			before := hits.Load()
			if err := request(wrong); err == nil || hits.Load() != before {
				t.Fatal("a wrong Reality public key reached the origin")
			}
		})
	}
}
