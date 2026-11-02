package xray

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func availablePort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
		if err == nil {
			udp.Close()
			listener.Close()
			return port
		}
		listener.Close()
	}
	t.Fatal("could not select a test port free on TCP and UDP")
	return 0
}

// testNodeID is the node identity every real-core Xray test renders for. The
// adapter's durable core-run marker requires the node ID it starts the child for.
const testNodeID = "test-node"

func testState(port int, revision int64) desiredstate.State {
	return desiredstate.State{SchemaVersion: 1, Revision: revision, NodeID: testNodeID, Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}}
}
func socksRequest(t *testing.T, port int, target string) string {
	t.Helper()
	response, err := socksExchange(port, target)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

// socksExchange performs one SOCKS5 request through the listener on port and
// returns the origin response. It reports a failure instead of ending the test,
// so a caller can wait for a freshly started chain to actually serve.
func socksExchange(port int, target string) (string, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	hostname := parsed.Hostname()
	targetPort, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return "", err
	}
	connection, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.Write([]byte{5, 1, 0}); err != nil {
		return "", fmt.Errorf("SOCKS5 negotiation failed: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return "", fmt.Errorf("SOCKS5 negotiation failed: %w", err)
	}
	if reply[1] != 0 {
		return "", fmt.Errorf("SOCKS5 negotiation failed: reply %d", reply[1])
	}
	request := []byte{5, 1, 0, 1}
	request = append(request, net.ParseIP(hostname).To4()...)
	request = binary.BigEndian.AppendUint16(request, uint16(targetPort))
	if _, err := connection.Write(request); err != nil {
		return "", fmt.Errorf("SOCKS5 connect failed: %w", err)
	}
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return "", fmt.Errorf("SOCKS5 connect failed: %w", err)
	}
	if reply[1] != 0 {
		return "", fmt.Errorf("SOCKS5 connect failed: reply %d", reply[1])
	}
	if _, err := io.WriteString(connection, "GET / HTTP/1.0\r\nHost: test\r\n\r\n"); err != nil {
		return "", err
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		return string(response), err
	}
	return string(response), nil
}

func TestRealXrayForwardNativeValidationAndRollback(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY to run official Xray integration")
	}
	adapter := &Adapter{Path: binaryPath, Directory: t.TempDir(), NodeID: testNodeID}
	defer adapter.Close()
	ctx := context.Background()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "proxy-control-origin-response") }))
	defer origin.Close()
	firstPort := availablePort(t)
	first, err := Render(ctx, testState(firstPort, 1))
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Apply(ctx, first)
	if err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if !strings.Contains(socksRequest(t, firstPort, origin.URL), "proxy-control-origin-response") {
		t.Fatal("SOCKS5 did not reach origin")
	}
	invalid := first
	invalid.Revision = 2
	invalid.Data = []byte("not-json")
	if result, err := adapter.Apply(ctx, invalid); err == nil || result.Code != "validation_failed" {
		t.Fatal("invalid native config accepted", result, err)
	}
	if !strings.Contains(socksRequest(t, firstPort, origin.URL), "proxy-control-origin-response") {
		t.Fatal("native validation failure interrupted previous config")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	second, err := Render(ctx, testState(port, 3))
	if err != nil {
		t.Fatal(err)
	}
	result, err = adapter.Apply(ctx, second)
	if err == nil || !result.RolledBack {
		t.Fatalf("port conflict did not rollback: %+v %v", result, err)
	}
	if !strings.Contains(socksRequest(t, firstPort, origin.URL), "proxy-control-origin-response") {
		t.Fatal("automatic rollback failed to restore traffic")
	}
	health, err := adapter.Health(ctx)
	if err != nil || !health.Healthy {
		t.Fatal(health, err)
	}
}
