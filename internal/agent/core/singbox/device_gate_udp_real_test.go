package singbox

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
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

// L8: Hysteria2 and TUIC carry UDP as QUIC datagrams/streams, so the TCP-stream
// gate test does not show that a gated user's UDP stops. This one keeps one SOCKS
// UDP association open through a client core and checks that the established
// flow stops within the documented ~2 s close interval, then records how much
// UDP still passes over the following seconds.
func TestRealDeviceGateSingboxQUICUDPDatagrams(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("awaiting CI/Owner run: SINGBOX_BINARY")
	}
	for _, protocol := range []string{"hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			source := testutil.GateSource(t)
			origin := udpEchoServer(t)
			s := stateFixture(t, protocol, freePort(t))
			s.Inbounds[0].Settings = localTLS(t)
			s.Inbounds[0].Settings.Transport = "quic"
			s.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{UpMbps: 100, DownMbps: 100}
			if protocol == "tuic" {
				s.Credentials[0].Username = "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
				s.Inbounds[0].Settings.QUIC = &desiredstate.QUICSettings{CongestionControl: "bbr"}
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
			clientState := stateFixture(t, "socks5", freePort(t))
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
			established := socksUDPAssociate(t, proxy, local.Username, local.Secret)
			if !established.exchange(origin) {
				t.Fatal("baseline UDP over " + protocol + " failed")
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				sample, err := server.ConnectionSnapshot(ctx)
				if err == nil && len(sample.Accounts) > 0 && len(sample.Accounts[0].AddressKeys) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("UDP source/user join unavailable")
				}
				time.Sleep(20 * time.Millisecond)
			}
			command, _ := json.Marshal(map[string]any{"ttl_seconds": 180, "entries": []map[string]any{{"credential_id": s.Credentials[0].ID, "keys": []string{source.String()}}}})
			started := time.Now()
			if err := server.SetDeviceGate(ctx, command); err != nil {
				t.Fatal(err)
			}
			stopped := false
			for time.Since(started) < 3*time.Second {
				if !established.exchange(origin) {
					stopped = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !stopped {
				t.Fatal("established UDP flow kept passing 3 s after the gate")
			}
			t.Logf("L8 %s: established UDP flow stopped %s after the gate", protocol, time.Since(started).Round(10*time.Millisecond))
			// Measure what still passes on the old and on a fresh association
			// while the gate stays in force; the result is evidence for L8.
			fresh := socksUDPAssociate(t, proxy, local.Username, local.Secret)
			passedOld, passedFresh, probes := 0, 0, 0
			for window := time.Now(); time.Since(window) < 6*time.Second; probes++ {
				if established.exchange(origin) {
					passedOld++
				}
				if fresh.exchange(origin) {
					passedFresh++
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Logf("L8 %s: over 6 s with the gate in force, %d/%d probes passed on the old association and %d/%d on a fresh one", protocol, passedOld, probes, passedFresh, probes)
		})
	}
}

// udpEchoServer returns the address of a loopback UDP server that echoes every packet.
func udpEchoServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	origin, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { origin.Close() })
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
	return origin.LocalAddr().(*net.UDPAddr)
}

// socksAssociation is one authenticated SOCKS5 UDP association that stays open,
// so the same flow can be probed before and after a gate.
type socksAssociation struct {
	udp  *net.UDPConn
	next byte
}

func socksUDPAssociate(t *testing.T, proxy, user, password string) *socksAssociation {
	t.Helper()
	control, err := net.DialTimeout("tcp", proxy, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { control.Close() })
	control.SetDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 2)
	if _, err := control.Write([]byte{5, 1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(control, reply); err != nil || reply[1] != 2 {
		t.Fatal("SOCKS proxy refused username/password authentication", reply, err)
	}
	login := append([]byte{1, byte(len(user))}, user...)
	login = append(append(login, byte(len(password))), password...)
	if _, err := control.Write(login); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(control, reply); err != nil || reply[1] != 0 {
		t.Fatal("SOCKS authentication failed", reply, err)
	}
	if _, err := control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(control, head); err != nil || head[1] != 0 {
		t.Fatal("SOCKS UDP ASSOCIATE refused", head, err)
	}
	size := map[byte]int{1: 4, 4: 16}[head[3]]
	if size == 0 {
		t.Fatal("unexpected SOCKS bind address type", head[3])
	}
	tail := make([]byte, size+2)
	if _, err := io.ReadFull(control, tail); err != nil {
		t.Fatal(err)
	}
	control.SetDeadline(time.Time{})
	relay := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(binary.BigEndian.Uint16(tail[size:]))}
	udp, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udp.Close() })
	return &socksAssociation{udp: udp}
}

// exchange sends one datagram carrying a fresh marker and reports whether its
// echo came back within 500 ms; stale echoes of earlier probes are skipped.
func (a *socksAssociation) exchange(target *net.UDPAddr) bool {
	a.next++
	marker := a.next
	packet := append([]byte{0, 0, 0, 1}, target.IP.To4()...)
	packet = binary.BigEndian.AppendUint16(packet, uint16(target.Port))
	packet = append(packet, 'l', '8', marker)
	if _, err := a.udp.Write(packet); err != nil {
		return false
	}
	a.udp.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	b := make([]byte, 1024)
	for {
		count, err := a.udp.Read(b)
		if err != nil {
			return false
		}
		if count > 10 && b[count-1] == marker && b[count-2] == '8' {
			return true
		}
	}
}
