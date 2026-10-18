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
	"strconv"
	"testing"
	"time"
)

func TestRealWireGuardDevicePolicyUsesAssignedSourceAddress(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for WireGuard policy integration")
	}
	address := ""
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ips, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, value := range ips {
			ip, _, err := net.ParseCIDR(value.String())
			if err == nil && ip.To4() != nil && ip.IsPrivate() {
				address = ip.String()
				break
			}
		}
		if address != "" {
			break
		}
	}
	if address == "" {
		t.Skip("non-loopback local IPv4 required for virtual WireGuard target")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(address, "0"))
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "wireguard-policy-origin") }))
	origin.Listener = listener
	origin.Start()
	defer origin.Close()
	serverKey := proxyCredentials(t, "wireguard")
	first := proxyCredentials(t, "wireguard")
	first.ID = "first-peer"
	second := proxyCredentials(t, "wireguard")
	second.ID = "second-peer"
	serverPort := availablePort(t)
	server := testState(serverPort, 1)
	server.Users = []desiredstate.User{{ID: "user", Enabled: true}}
	server.Credentials = []desiredstate.Credential{{ID: first.ID, Protocol: "wireguard", UserID: "user", DeviceID: "first", PublicKey: publicKey(t, first.Secret)}, {ID: second.ID, Protocol: "wireguard", UserID: "user", DeviceID: "second", PublicKey: publicKey(t, second.Secret)}}
	server.Inbounds[0].Protocol = "wireguard"
	server.Inbounds[0].CredentialIDs = []string{first.ID, second.ID}
	server.Inbounds[0].Settings = desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: serverKey.Secret, Addresses: []string{"10.91.0.1/24"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: publicKey(t, first.Secret), AllowedIPs: []string{"10.91.0.2/32"}, CredentialID: first.ID}, {PublicKey: publicKey(t, second.Secret), AllowedIPs: []string{"10.91.0.3/32"}, CredentialID: second.ID}}}}
	server.Policies = []desiredstate.Policy{{ID: "first-peer-block", Priority: 1, Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{"user"}, DeviceIDs: []string{"first"}}, Match: desiredstate.Match{Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}
	events := make(chan access.Rollup, 16)
	adapter := &Adapter{Path: os.Getenv("XRAY_BINARY"), Directory: t.TempDir(), NodeID: server.NodeID, EnableStats: true, AccessSink: func(record access.Rollup) error { events <- record; return nil }}
	defer adapter.Close()
	rendered, err := adapter.Render(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(context.Background(), rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	for index, credential := range []desiredstate.Credential{first, second} {
		localPort := availablePort(t)
		client := testState(localPort, 1)
		client.Credentials = []desiredstate.Credential{credential}
		client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "wireguard-entry", Protocol: "wireguard", Address: "127.0.0.1", Port: serverPort, Enabled: true, CredentialID: credential.ID, Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: credential.Secret, Addresses: []string{"10.91.0." + strconv.Itoa(index+2) + "/32"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: publicKey(t, serverKey.Secret), Endpoint: "127.0.0.1:" + strconv.Itoa(serverPort), AllowedIPs: []string{"0.0.0.0/0"}, KeepAlive: 15}}}}})
		client.Inbounds[0].DefaultOutbound = "wireguard-entry"
		startState(t, client)
		_, allowed := domainRequest(t, localPort, address, origin.Listener.Addr().(*net.TCPAddr).Port)
		if allowed != (index == 1) {
			t.Fatalf("WireGuard scoped policy wrong for peer %d", index)
		}
		select {
		case record := <-events:
			if record.UserID != "user" || record.DeviceID != []string{"first", "second"}[index] || record.Allowed != (index == 1) {
				t.Fatal("WireGuard peer telemetry attribution wrong", record)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("WireGuard telemetry missing")
		}
	}
	counters, err := adapter.TrafficCounters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondMeasured := false
	for _, counter := range counters {
		if counter.DeviceID == "second" && counter.DownloadBytes > 0 {
			secondMeasured = true
		}
	}
	if !secondMeasured {
		t.Fatal("WireGuard device byte counter missing", counters)
	}
}
