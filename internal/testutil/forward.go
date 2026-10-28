package testutil

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

// RealForward checks a native core's TCP and UDP forwarding to local echo
// listeners. Callers must gate it on their binary environment variable.
func RealForward(t *testing.T, binary string, args []string, render func(desiredstate.State) ([]byte, error)) {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	targetPort := tcp.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(targetPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, a, e := udp.ReadFrom(buf)
			if e != nil {
				return
			}
			udp.WriteTo(buf[:n], a)
		}
	}()
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	reservation.Close()
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "forward-test", Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "forward", Protocol: "forward", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "127.0.0.1", Port: targetPort, Network: "tcp,udp"}}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	data, err := render(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append(args, path)...)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var conn net.Conn
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); {
		conn, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte("forward-echo")
	if _, err = conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, got); err != nil || string(got) != string(payload) {
		t.Fatalf("TCP echo %q: %v", got, err)
	}
	packet, err := net.DialTimeout("udp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	packet.SetDeadline(time.Now().Add(3 * time.Second))
	packet.Write(payload)
	n, err := packet.Read(got)
	if err != nil || string(got[:n]) != string(payload) {
		t.Fatalf("UDP echo %q: %v", got[:n], err)
	}
}
