package xray

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
)

func publicKey(t *testing.T, secret string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdh.X25519().NewPrivateKey(data)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}
func TestRealWireGuardInboundOutboundForwarding(t *testing.T) {
	if os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set XRAY_BINARY for WireGuard integration")
	}
	serverKey := proxyCredentials(t, "wireguard")
	clientKey := proxyCredentials(t, "wireguard")
	clientKey.ID = "wg-client"
	port := availablePort(t)
	server := testState(port, 1)
	server.NodeID = "wg-server"
	server.Credentials = []desiredstate.Credential{clientKey}
	server.Inbounds[0].Protocol = "wireguard"
	server.Inbounds[0].CredentialIDs = []string{clientKey.ID}
	server.Inbounds[0].Settings = desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: serverKey.Secret, Addresses: []string{"10.66.0.1/32"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: publicKey(t, clientKey.Secret), AllowedIPs: []string{"10.66.0.2/32"}, CredentialID: clientKey.ID}}}}
	startState(t, server)
	localPort := availablePort(t)
	client := testState(localPort, 1)
	client.NodeID = "wg-client-node"
	client.Credentials = []desiredstate.Credential{clientKey}
	client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "wg-out", Protocol: "wireguard", Enabled: true, Address: "127.0.0.1", Port: port, CredentialID: clientKey.ID, TargetNodeID: "wg-server", Settings: desiredstate.ProtocolSettings{WireGuard: &desiredstate.WireGuardSettings{SecretKey: clientKey.Secret, Addresses: []string{"10.66.0.2/32"}, Peers: []desiredstate.WireGuardPeer{{PublicKey: publicKey(t, serverKey.Secret), Endpoint: "127.0.0.1:" + strconv.Itoa(port), AllowedIPs: []string{"0.0.0.0/0"}, KeepAlive: 15}}}}})
	client.Inbounds[0].DefaultOutbound = "wg-out"
	adapter := startState(t, client)
	var originAddress string
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && ip.IsPrivate() {
				originAddress = ip.String()
				break
			}
		}
		if originAddress != "" {
			break
		}
	}
	if originAddress == "" {
		t.Skip("a local non-loopback IPv4 is required for WireGuard forwarding; virtual loopback is not tunneled")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(originAddress, "0"))
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "wireguard-forwarded-origin") }))
	origin.Listener = listener
	origin.Start()
	defer origin.Close()
	if !strings.Contains(socksRequest(t, localPort, origin.URL), "wireguard-forwarded-origin") {
		t.Fatal("WireGuard tunnel did not forward")
	}
	if health, err := adapter.Health(context.Background()); err != nil || !health.Healthy {
		t.Fatal(err)
	}
}
