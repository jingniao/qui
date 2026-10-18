package xray

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
)

func proxyCredentials(t *testing.T, protocol string) desiredstate.Credential {
	t.Helper()
	secret, err := user.GenerateCredential(protocol)
	if err != nil {
		t.Fatal(err)
	}
	return desiredstate.Credential{ID: "credential-" + protocol, Protocol: protocol, Secret: secret.Value, Username: "relay-user", Internal: true}
}
func startState(t *testing.T, state desiredstate.State) *Adapter {
	t.Helper()
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: state.NodeID}
	rendered, err := Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Apply(context.Background(), rendered)
	if err != nil {
		t.Fatal(result, err)
	}
	t.Cleanup(func() { adapter.Close() })
	return adapter
}
func tlsMaterials(t *testing.T) desiredstate.ProtocolSettings {
	t.Helper()
	pki := testutil.Certificates(t)
	certificate, err := os.ReadFile(pki.Server.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(pki.Server.Key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(pki.Server.ClientCA)
	if err != nil {
		t.Fatal(err)
	}
	return desiredstate.ProtocolSettings{Security: "tls", ServerName: "localhost", CertificatePEM: string(certificate), PrivateKeyPEM: string(key), TrustedCAPEM: string(ca)}
}
func clientTLS(settings desiredstate.ProtocolSettings) desiredstate.ProtocolSettings {
	settings.CertificatePEM = ""
	settings.PrivateKeyPEM = ""
	return settings
}
func TestRealAllProtocolInboundOutboundAndConversion(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for real protocol integration")
	}
	for _, scenario := range []struct {
		name, entry, relay string
		tls                bool
	}{{"VLESS to direct", "vless", "", false}, {"VLESS TLS to Trojan relay", "vless", "trojan", true}, {"Shadowsocks to SOCKS5 relay", "shadowsocks", "socks5", false}, {"SOCKS5 to HTTP relay", "socks5", "http", false}, {"Trojan to VLESS TLS relay", "trojan", "vless", true}, {"SOCKS5 to Shadowsocks relay", "socks5", "shadowsocks", false}} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "protocol-conversion-origin") }))
			defer origin.Close()
			entryCredential := proxyCredentials(t, scenario.entry)
			entryPort := availablePort(t)
			entry := testState(entryPort, 1)
			entry.NodeID = "node-a"
			entry.Inbounds[0].Protocol = scenario.entry
			entry.Inbounds[0].CredentialIDs = []string{entryCredential.ID}
			entry.Credentials = []desiredstate.Credential{entryCredential}
			settings := desiredstate.ProtocolSettings{}
			if scenario.tls || scenario.entry == "trojan" {
				settings = tlsMaterials(t)
				entry.Inbounds[0].Settings = settings
			}
			if scenario.relay != "" {
				relayCredential := proxyCredentials(t, scenario.relay)
				relayPort := availablePort(t)
				relay := testState(relayPort, 1)
				relay.NodeID = "node-b"
				relay.Inbounds[0].ID = "relay-in"
				relay.Inbounds[0].Protocol = scenario.relay
				relay.Inbounds[0].CredentialIDs = []string{relayCredential.ID}
				relay.Credentials = []desiredstate.Credential{relayCredential}
				relaySettings := desiredstate.ProtocolSettings{}
				if scenario.relay == "trojan" || scenario.tls {
					relaySettings = tlsMaterials(t)
					relay.Inbounds[0].Settings = relaySettings
				}
				startState(t, relay)
				entry.Credentials = append(entry.Credentials, relayCredential)
				entry.Outbounds = append(entry.Outbounds, desiredstate.Outbound{ID: "relay-out", Protocol: scenario.relay, Address: "127.0.0.1", Port: relayPort, Enabled: true, CredentialID: relayCredential.ID, TargetNodeID: "node-b", Settings: clientTLS(relaySettings)})
				entry.Inbounds[0].DefaultOutbound = "relay-out"
			}
			startState(t, entry)
			// A local SOCKS client reaches the entry through its declared protocol.
			localPort := availablePort(t)
			client := testState(localPort, 1)
			client.NodeID = "client"
			client.Credentials = []desiredstate.Credential{entryCredential}
			client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "entry-out", Protocol: scenario.entry, Address: "127.0.0.1", Port: entryPort, Enabled: true, CredentialID: entryCredential.ID, Settings: clientTLS(settings)})
			client.Inbounds[0].DefaultOutbound = "entry-out"
			startState(t, client)
			if !strings.Contains(socksRequest(t, localPort, origin.URL), "protocol-conversion-origin") {
				t.Fatal("protocol conversion did not reach origin")
			}
			if _, err := Render(ctx, entry); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestRealMultiUserShadowsocksNativeValidation(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for native validation")
	}
	first := proxyCredentials(t, "shadowsocks")
	second := proxyCredentials(t, "shadowsocks")
	second.ID = "second-device"
	state := testState(availablePort(t), 1)
	state.Inbounds[0].Protocol = "shadowsocks"
	state.Inbounds[0].CredentialIDs = []string{first.ID, second.ID}
	state.Credentials = []desiredstate.Credential{first, second}
	adapter := startState(t, state)
	health, err := adapter.Health(context.Background())
	if err != nil || !health.Healthy {
		t.Fatal(err)
	}
	rendered, err := Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(rendered.Data, &document); err != nil {
		t.Fatal(err)
	}
}
