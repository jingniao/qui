package singbox

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func TestNativeHysteria2AndTUICAuthenticatedTCPAndUDP(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for actual QUIC protocols")
	}
	for _, protocol := range []string{"hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			packet, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			peerPort := packet.LocalAddr().(*net.UDPAddr).Port
			packet.Close()
			port := func() int {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port := listener.Addr().(*net.TCPAddr).Port
				listener.Close()
				return port
			}
			clientPort := port()
			udpClientPort := port()
			peer := stateFixture(t, protocol, peerPort)
			peer.Inbounds[0].Listen = "0.0.0.0"
			peer.Inbounds[0].Settings = localTLS(t)
			peer.Inbounds[0].Settings.Transport = "quic"
			if protocol == "hysteria2" {
				peer.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{UpMbps: 100, DownMbps: 100}
			} else {
				peer.Credentials[0].Username = "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
				peer.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{CongestionControl: "bbr"}
			}
			peerAdapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: peer.NodeID}
			defer peerAdapter.Close()
			rendered, err := peerAdapter.Render(context.Background(), peer)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := peerAdapter.Apply(context.Background(), rendered); err != nil || !result.Success {
				t.Fatal("QUIC server did not start", result, err)
			}
			client := stateFixture(t, "http", clientPort)
			client.NodeID = "quic-client"
			client.Credentials = append(client.Credentials, desiredstate.Credential{ID: "quic-upstream", Protocol: protocol, Internal: true, Username: peer.Credentials[0].Username, Secret: peer.Credentials[0].Secret}, desiredstate.Credential{ID: "udp-client", UserID: "user", DeviceID: "udp-device", Protocol: "socks5", Username: "udp-user", Secret: "udp-auth-password"})
			client.Inbounds[0].DefaultOutbound = "quic-out"
			client.Inbounds = append(client.Inbounds, desiredstate.Inbound{ID: "udp-entry", Protocol: "socks5", Listen: "127.0.0.1", Port: udpClientPort, Enabled: true, CredentialIDs: []string{"udp-client"}, DefaultOutbound: "quic-out"})
			settings := desiredstate.ProtocolSettings{Transport: "quic", Security: "tls", ServerName: "localhost", TrustedCAPEM: peer.Inbounds[0].Settings.CertificatePEM, QUIC: &desiredstate.QUICSettings{}}
			if protocol == "hysteria2" {
				settings.QUIC.UpMbps = 100
				settings.QUIC.DownMbps = 100
			} else {
				settings.QUIC.CongestionControl = "bbr"
				settings.QUIC.UDPRelayMode = "native"
			}
			client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "quic-out", Protocol: protocol, Address: "127.0.0.1", Port: peerPort, Enabled: true, CredentialID: "quic-upstream", Settings: settings})
			clientAdapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: client.NodeID}
			defer clientAdapter.Close()
			rendered, err = clientAdapter.Render(context.Background(), client)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := clientAdapter.Apply(context.Background(), rendered); err != nil || !result.Success {
				t.Fatal("QUIC client did not start", result, err)
			}
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "quic-real-forwarding") }))
			defer origin.Close()
			proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(clientPort))
			proxyURL.User = url.UserPassword("sing-user", client.Credentials[0].Secret)
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get(origin.URL)
			if err != nil {
				t.Fatal("QUIC TCP forwarding failed", err)
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || string(data) != "quic-real-forwarding" {
				t.Fatal("unexpected QUIC response", string(data), err)
			}
			udpEcho(t, udpClientPort)
			client.Revision = 2
			client.Credentials[1].Secret = "incorrect-quic-password"
			bad, err := clientAdapter.Render(context.Background(), client)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := clientAdapter.Apply(context.Background(), bad); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			response, err = (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
			if err == nil {
				data, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if string(data) == "quic-real-forwarding" {
					t.Fatal("invalid QUIC credentials forwarded traffic")
				}
			}
		})
	}
}

func udpEcho(t *testing.T, proxyPort int, authenticated ...bool) {
	t.Helper()
	origin, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, address, err := origin.ReadFrom(buffer)
			if err != nil {
				return
			}
			origin.WriteTo(buffer[:n], address)
		}
	}()
	control, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(proxyPort), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.SetDeadline(time.Now().Add(5 * time.Second))
	method := byte(2)
	if len(authenticated) > 0 && !authenticated[0] {
		method = 0
	}
	control.Write([]byte{5, 1, method})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(control, reply); err != nil || reply[1] != method {
		t.Fatal("SOCKS UDP auth not selected", reply, err)
	}
	if method == 2 {
		user, password := []byte("udp-user"), []byte("udp-auth-password")
		auth := append([]byte{1, byte(len(user))}, user...)
		auth = append(auth, byte(len(password)))
		auth = append(auth, password...)
		control.Write(auth)
		if _, err := io.ReadFull(control, reply); err != nil || reply[1] != 0 {
			t.Fatal("UDP control auth failed", err)
		}
	}
	control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	header := make([]byte, 4)
	if _, err := io.ReadFull(control, header); err != nil || header[1] != 0 || header[3] != 1 {
		t.Fatal("IPv4 UDP association failed", header, err)
	}
	address := make([]byte, 6)
	if _, err := io.ReadFull(control, address); err != nil {
		t.Fatal(err)
	}
	relay := &net.UDPAddr{IP: net.IP(address[:4]), Port: int(address[4])<<8 | int(address[5])}
	if relay.IP.IsUnspecified() {
		relay.IP = net.ParseIP("127.0.0.1")
	}
	socket, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	socket.SetDeadline(time.Now().Add(5 * time.Second))
	target := origin.LocalAddr().(*net.UDPAddr)
	packet := append([]byte{0, 0, 0, 1, 127, 0, 0, 1, byte(target.Port >> 8), byte(target.Port)}, []byte("native-quic-udp")...)
	if _, err := socket.Write(packet); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1024)
	n, err := socket.Read(buffer)
	if err != nil || n < 10 || string(buffer[10:n]) != "native-quic-udp" {
		t.Fatal("QUIC UDP forwarding failed", n, err)
	}
}
