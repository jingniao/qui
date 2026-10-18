package xray

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	access "proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func socksUDP(t *testing.T, port int, target *net.UDPAddr) {
	t.Helper()
	control, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.SetDeadline(time.Now().Add(5 * time.Second))
	control.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(control, reply); err != nil {
		t.Fatal(err)
	}
	control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	reply = make([]byte, 10)
	if _, err := io.ReadFull(control, reply); err != nil || reply[1] != 0 {
		t.Fatal("UDP associate rejected", err)
	}
	boundPort := int(binary.BigEndian.Uint16(reply[8:10]))
	connection, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: boundPort})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	packet := binary.BigEndian.AppendUint16([]byte{0, 0, 0, 1, 127, 0, 0, 1}, uint16(target.Port))
	packet = append(packet, []byte("udp-proxy-control")...)
	if _, err := connection.Write(packet); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 1024)
	count, err := connection.Read(response)
	if err != nil || count < 10 || string(response[10:count]) != "udp-proxy-control" {
		t.Fatal("UDP forwarding failed", err)
	}
}
func TestRealUDPProtocolForwarding(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for UDP integration")
	}
	for _, protocol := range []string{"vless", "trojan", "shadowsocks", "socks5"} {
		t.Run(protocol, func(t *testing.T) {
			origin, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
			if err != nil {
				t.Fatal(err)
			}
			defer origin.Close()
			go func() {
				buffer := make([]byte, 2048)
				for {
					count, peer, err := origin.ReadFromUDP(buffer)
					if err != nil {
						return
					}
					origin.WriteToUDP(buffer[:count], peer)
				}
			}()
			credential := proxyCredentials(t, protocol)
			credential.UserID = "udp-user"
			credential.DeviceID = "udp-device"
			credential.Internal = false
			serverPort := availablePort(t)
			server := testState(serverPort, 1)
			server.Users = []desiredstate.User{{ID: "udp-user", Enabled: true}}
			server.Credentials = []desiredstate.Credential{credential}
			server.Inbounds[0].Protocol = protocol
			server.Inbounds[0].CredentialIDs = []string{credential.ID}
			settings := desiredstate.ProtocolSettings{}
			if protocol == "trojan" {
				settings = tlsMaterials(t)
				server.Inbounds[0].Settings = settings
			}
			events := make(chan access.Rollup, 16)
			adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: server.NodeID, AccessSink: func(record access.Rollup) error { events <- record; return nil }}
			defer adapter.Close()
			rendered, err := adapter.Render(context.Background(), server)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := adapter.Apply(context.Background(), rendered); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			localPort := availablePort(t)
			client := testState(localPort, 1)
			client.Users = []desiredstate.User{{ID: "udp-user", Enabled: true}}
			client.Credentials = []desiredstate.Credential{credential}
			client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "entry", Protocol: protocol, Address: "127.0.0.1", Port: serverPort, Enabled: true, CredentialID: credential.ID, Settings: clientTLS(settings)})
			client.Inbounds[0].DefaultOutbound = "entry"
			startState(t, client)
			socksUDP(t, localPort, origin.LocalAddr().(*net.UDPAddr))
			select {
			case record := <-events:
				if record.Network != "udp" || !record.Allowed {
					t.Fatal("UDP access telemetry wrong", record)
				}
				if protocol == "socks5" {
					if record.UserID != "" || record.DeviceID != "" {
						t.Fatal("SOCKS UDP identity was guessed")
					}
				} else if record.UserID != "udp-user" || record.DeviceID != "udp-device" {
					t.Fatal("UDP identity not preserved", record)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("UDP access telemetry missing", protocol)
			}
		})
	}
}
