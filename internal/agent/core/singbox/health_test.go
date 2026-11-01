package singbox

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"testing"

	"proxycontrol/internal/agent/core"
)

// A listener that does not answer is its own branch: the supervisor restarts a
// core only after consecutive listener failures, because the dial can fail while
// the core still serves. Both listener families report it.
func TestHealthReportsAListenerFailure(t *testing.T) {
	ctx := context.Background()
	closed := func(t *testing.T, packet bool) string {
		t.Helper()
		if packet {
			socket, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			endpoint := socket.LocalAddr().String()
			socket.Close()
			return endpoint
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		endpoint := listener.Addr().String()
		listener.Close()
		return endpoint
	}
	for _, tc := range []struct {
		name   string
		packet bool
	}{
		{name: "tcp"},
		{name: "udp", packet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := closed(t, tc.packet)
			configuration := core.RenderedConfig{HealthEndpoints: []string{endpoint}}
			if tc.packet {
				configuration = core.RenderedConfig{UDPHealthEndpoints: []string{endpoint}}
			}
			adapter := &Adapter{Directory: t.TempDir(), process: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, current: configuration}
			if _, err := adapter.Health(ctx); !errors.Is(err, core.ErrListenerUnavailable) {
				t.Fatalf("%s listener failure: %v", tc.name, err)
			}
		})
	}
}
