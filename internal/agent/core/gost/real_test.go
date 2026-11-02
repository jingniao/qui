package gost

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// TestRealApplySettleRollbackAndStats runs the adapter against the pinned binary.
// It covers G1's whole apply path: the syntax gate, the restart, the two-second
// settle gate, TCP and UDP forwarding, the node counter read from the private
// metrics socket, and the rollback of a revision whose port cannot be bound.
func TestRealApplySettleRollbackAndStats(t *testing.T) {
	binary := os.Getenv("GOST_BINARY")
	if binary == "" {
		t.Skip("GOST_BINARY is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	target := echoTarget(t)
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: "node-gost"}
	saved := []traffic.Counter{}
	adapter.ConfigureRuntime(core.RuntimeHooks{TrafficSink: func(_ context.Context, counters []traffic.Counter) error {
		saved = append(saved, counters...)
		return nil
	}})
	defer adapter.Close()
	port := freePort(t)
	rendered, err := adapter.Render(ctx, forwardState(1, port, target))
	if err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	result, err := adapter.Apply(ctx, rendered)
	if err != nil || !result.Success {
		t.Fatalf("%+v %v %q", result, err, adapter.log.tail())
	}
	echo := func(t *testing.T, address string) {
		t.Helper()
		connection, err := net.DialTimeout("tcp", address, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := connection.Write([]byte("gost-forward")); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len("gost-forward"))
		if _, err := io.ReadFull(connection, got); err != nil || string(got) != "gost-forward" {
			t.Fatalf("tcp echo %q: %v", got, err)
		}
		packet, err := net.DialTimeout("udp", address, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer packet.Close()
		packet.SetDeadline(time.Now().Add(5 * time.Second))
		packet.Write([]byte("gost-forward"))
		n, err := packet.Read(got)
		if err != nil || string(got[:n]) != "gost-forward" {
			t.Fatalf("udp echo %q: %v", got[:n], err)
		}
	}
	echo(t, address)
	if status, err := adapter.Health(ctx); err != nil || !status.Healthy {
		t.Fatalf("%+v %v", status, err)
	}
	// The counters are cumulative since the process start; the panel cursors the
	// per-epoch total, so a positive reading is what this test can require.
	if err := adapter.SampleTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Kind != "node" || saved[0].CredentialID != "node" || !strings.HasSuffix(saved[0].CoreEpoch, ".node") || saved[0].UploadBytes <= 0 || saved[0].DownloadBytes <= 0 {
		t.Fatalf("node counter: %+v", saved)
	}
	if total, err := adapter.CollectStats(ctx); err != nil || total.Upload == 0 || total.Download == 0 {
		t.Fatalf("%+v %v", total, err)
	}
	// A revision whose listener cannot bind must fail the settle gate and restore
	// the revision that was serving.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	broken, err := adapter.Render(ctx, forwardState(2, occupied.Addr().(*net.TCPAddr).Port, target))
	if err != nil {
		t.Fatal(err)
	}
	result, err = adapter.Apply(ctx, broken)
	if err == nil || result.Success || !result.RolledBack || result.Code != "start_failed" {
		t.Fatalf("%+v %v", result, err)
	}
	echo(t, address)
}

// echoTarget starts a TCP and a UDP echo listener on one loopback port, the way a
// forwarded target answers.
func echoTarget(t *testing.T) int {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := tcp.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tcp.Close()
		udp.Close()
	})
	go func() {
		for {
			connection, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() { defer connection.Close(); io.Copy(connection, connection) }()
		}
	}()
	go func() {
		buffer := make([]byte, 1024)
		for {
			read, from, err := udp.ReadFrom(buffer)
			if err != nil {
				return
			}
			udp.WriteTo(buffer[:read], from)
		}
	}()
	return port
}

// freePort reserves a loopback port and releases it for the forward. The revert of
// that release is the settle gate's job, as it is for every other core.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// forwardState is the desired state of one forward that serves TCP and UDP.
func forwardState(revision int64, port, target int) desiredstate.State {
	return desiredstate.State{
		SchemaVersion: 1,
		Revision:      revision,
		NodeID:        "node-gost",
		Logging:       desiredstate.Logging{DomainOnly: true},
		Inbounds: []desiredstate.Inbound{
			forwardInbound("real-forward", "127.0.0.1", port, "127.0.0.1", target, "tcp,udp", true),
		},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
}

// TestApplyRejectsABrokenRevisionBeforeTouchingTheCore covers the first of the two
// GOST validation stages: a revision that is not a native configuration fails where
// it is validated, and no process is stopped, written or started for it.
func TestApplyRejectsABrokenRevisionBeforeTouchingTheCore(t *testing.T) {
	adapter := &Adapter{Path: "gost-does-not-exist", Directory: t.TempDir(), NodeID: "node-gost"}
	result, err := adapter.Apply(context.Background(), core.RenderedConfig{Revision: 4, Data: []byte("{")})
	if err == nil || result.Success || result.Code != "validation_failed" || result.Revision != 4 {
		t.Fatalf("%+v %v", result, err)
	}
	if adapter.process != nil || adapter.UnconfirmedStop() {
		t.Fatal("a rejected revision changed the core lifetime")
	}
	if _, err := os.Stat(filepath.Join(adapter.Directory, ConfigFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a rejected revision wrote the live configuration: %v", err)
	}
}
