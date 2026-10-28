package xray

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"proxycontrol/internal/testutil"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
)

func TestRealDeviceGateXrayUDPProtocolsAndSocksException(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("awaiting CI/Owner run: XRAY_BINARY")
	}
	source := testutil.GateSource(t)
	for _, protocol := range []string{"vless", "trojan", "shadowsocks", "socks5"} {
		t.Run(protocol, func(t *testing.T) {
			origin, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
			if err != nil {
				t.Fatal(err)
			}
			defer origin.Close()
			go func() {
				b := make([]byte, 1024)
				for {
					n, peer, err := origin.ReadFromUDP(b)
					if err != nil {
						return
					}
					origin.WriteToUDP(b[:n], peer)
				}
			}()
			credential := proxyCredentials(t, protocol)
			credential.Internal = false
			credential.UserID = "u"
			credential.DeviceID = "d"
			server := testState(availablePort(t), 1)
			server.Users = []desiredstate.User{{ID: "u", Enabled: true}}
			server.Credentials = []desiredstate.Credential{credential}
			server.Inbounds[0].Protocol = protocol
			server.Inbounds[0].CredentialIDs = []string{credential.ID}
			if protocol == "trojan" {
				server.Inbounds[0].Settings = tlsMaterials(t)
			}
			a := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: server.NodeID, EnableStats: true}
			defer a.Close()
			ctx := context.Background()
			config, err := a.Render(ctx, server)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := a.Apply(ctx, config); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			client := testState(availablePort(t), 1)
			client.Users = server.Users
			client.Credentials = []desiredstate.Credential{credential}
			client.Network = &desiredstate.Network{OutboundSourceIP: source.String()}
			client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "remote", Protocol: protocol, Address: "127.0.0.1", Port: server.Inbounds[0].Port, Enabled: true, CredentialID: credential.ID, Settings: clientTLS(server.Inbounds[0].Settings)})
			client.Inbounds[0].DefaultOutbound = "remote"
			startState(t, client)
			proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(client.Inbounds[0].Port))
			target := origin.LocalAddr().String()
			if !testutil.GateUDPExchange(proxy, target) {
				t.Fatal("baseline UDP did not work")
			}
			command, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []map[string]any{{"credential_id": credential.ID, "keys": []string{source.String()}}}})
			if err := a.SetDeviceGate(ctx, command); err != nil {
				t.Fatal(err)
			}
			allowed := testutil.GateUDPExchange(proxy, target)
			if allowed != (protocol == "socks5") {
				t.Fatal("UDP gate result disagrees with the documented identity capability")
			}
		})
	}
	// Official Xray has no Hysteria2/TUIC inbounds (Phase 0 protocol matrix).
	// Their source/connection enforcement belongs to sing-box, not a core patch.
}
