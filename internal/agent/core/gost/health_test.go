package gost

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"proxycontrol/internal/agent/core"
)

// TestHealthReportsAListenerFailure pins the health contract of this core: an
// unanswered TCP dial and an unbound UDP socket are listener failures that name the
// listener and wrap core.ErrListenerUnavailable, so the supervisor waits for
// consecutive failures instead of restarting a core that may still be serving.
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
		family string
		packet bool
	}{
		{name: "tcp", family: "listener"},
		{name: "udp", family: "socket", packet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := closed(t, tc.packet)
			configuration := core.RenderedConfig{HealthEndpoints: []string{endpoint}}
			if tc.packet {
				configuration = core.RenderedConfig{UDPHealthEndpoints: []string{endpoint}}
			}
			adapter := &Adapter{Directory: t.TempDir(), process: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, current: configuration}
			_, err := adapter.Health(ctx)
			if !errors.Is(err, core.ErrListenerUnavailable) {
				t.Fatalf("%s listener failure: %v", tc.name, err)
			}
			if !strings.Contains(err.Error(), "gost "+tc.name+" "+tc.family+" "+endpoint+" unavailable") {
				t.Fatalf("%s listener failure did not name the listener: %v", tc.name, err)
			}
			if tc.packet {
				return
			}
			// A dial that failed keeps its own error for the restore log.
			var dial *net.OpError
			if !errors.As(err, &dial) {
				t.Fatalf("tcp listener failure lost its dial error: %v", err)
			}
		})
	}
}

// TestHealthReportsAStoppedCore separates "not running" from a listener failure: a
// stopped core is never healthy and never reported as a listener problem.
func TestHealthReportsAStoppedCore(t *testing.T) {
	adapter := &Adapter{Directory: t.TempDir()}
	if _, err := adapter.Health(context.Background()); err == nil || errors.Is(err, core.ErrListenerUnavailable) {
		t.Fatalf("stopped core health: %v", err)
	}
}

// An installed core that never ran has no epoch to attribute counters to; the
// panel accepts only "unavailable" without one.
func TestIdleCoreReportsUnavailableTrafficSource(t *testing.T) {
	status := (&Adapter{Directory: t.TempDir(), NodeID: "node"}).TrafficSourceStatus()
	if status.Status != "unavailable" || status.CoreEpoch != "" || status.Revision != 0 || status.CoreRunning || status.ConfiguredSources != 0 {
		t.Fatalf("%+v", status)
	}
}
