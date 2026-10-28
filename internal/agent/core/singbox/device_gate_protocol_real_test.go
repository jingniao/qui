package singbox

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func TestRealDeviceGateSingboxProtocolSourceJoins(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("awaiting CI/Owner run: SINGBOX_BINARY")
	}
	for _, protocol := range []string{"http", "socks5", "shadowsocks", "vless", "trojan", "hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			source := testutil.GateSource(t)
			target := testutil.GateEcho(t)
			s := stateFixture(t, protocol, freePort(t))
			if protocol == "hysteria2" || protocol == "tuic" {
				s.Inbounds[0].Settings = localTLS(t)
				s.Inbounds[0].Settings.Transport = "quic"
				s.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{UpMbps: 100, DownMbps: 100}
				if protocol == "tuic" {
					s.Credentials[0].Username = "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
					s.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{CongestionControl: "bbr"}
				}
			}
			server := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: s.NodeID}
			defer server.Close()
			server.ConfigureRuntime(core.RuntimeHooks{EnableStats: true, AccessSink: func(access.Rollup) error { return nil }})
			config, err := server.Render(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := server.Apply(ctx, config); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			clientState := stateFixture(t, "http", freePort(t))
			clientState.NodeID = "gate-client"
			clientState.Network = &desiredstate.Network{OutboundSourceIP: source.String()}
			credential := s.Credentials[0]
			credential.ID = "remote-credential"
			credential.Internal = true
			credential.UserID = ""
			credential.DeviceID = ""
			clientState.Credentials = append(clientState.Credentials, credential)
			settings := s.Inbounds[0].Settings
			settings.TrustedCAPEM = settings.CertificatePEM
			settings.CertificatePEM = ""
			settings.PrivateKeyPEM = ""
			clientState.Outbounds = append(clientState.Outbounds, desiredstate.Outbound{ID: "remote", Protocol: protocol, Enabled: true, Address: "127.0.0.1", Port: s.Inbounds[0].Port, CredentialID: credential.ID, Settings: settings})
			clientState.Inbounds[0].DefaultOutbound = "remote"
			client := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: clientState.NodeID}
			defer client.Close()
			rendered, err := client.Render(ctx, clientState)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := client.Apply(ctx, rendered); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(clientState.Inbounds[0].Port))
			local := clientState.Credentials[0]
			c, err := testutil.GateTunnel(proxy, target, local.Username, local.Secret, nil)
			if err != nil {
				t.Fatal("client tunnel failed")
			}
			defer c.Close()
			if !testutil.GateRoundTrip(c) {
				t.Fatal("baseline protocol failed")
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				sample, err := server.ConnectionSnapshot(ctx)
				if err == nil && len(sample.Accounts) > 0 && len(sample.Accounts[0].AddressKeys) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("protocol source/user join unavailable")
				}
				time.Sleep(20 * time.Millisecond)
			}
			command, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []map[string]any{{"credential_id": s.Credentials[0].ID, "keys": []string{source.String()}}}})
			started := time.Now()
			if err := server.SetDeviceGate(ctx, command); err != nil {
				t.Fatal(err)
			}
			if testutil.GateRoundTrip(c) || time.Since(started) > 3*time.Second {
				t.Fatal("protocol connection was not closed")
			}
		})
	}
}
