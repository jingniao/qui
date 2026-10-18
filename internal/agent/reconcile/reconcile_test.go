package reconcile

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func freePort(t *testing.T) int {
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
func state(port int, revision int64) desiredstate.State {
	return desiredstate.State{SchemaVersion: 1, Revision: revision, NodeID: "node-test", Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
}
func TestRealRevisionPersistenceRestartRollbackAndIdempotence(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set XRAY_BINARY for real Reconciler integration")
	}
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: path, Directory: filepath.Join(directory, "runtime")}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	first := state(freePort(t), 1)
	if result, err := manager.Apply(ctx, first); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if result, err := manager.Apply(ctx, first); err != nil || !result.Success {
		t.Fatal("duplicate apply failed", result, err)
	}
	changed := first
	changed.Inbounds = append([]desiredstate.Inbound(nil), first.Inbounds...)
	changed.Inbounds[0].Port = freePort(t)
	if _, err := manager.Apply(ctx, changed); err == nil {
		t.Fatal("changed immutable revision accepted")
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if health, err := adapter.Health(ctx); err != nil || !health.Healthy {
		t.Fatal("restart did not restore core", err)
	}
	second := state(freePort(t), 2)
	if result, err := restarted.Apply(ctx, second); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := restarted.Rollback(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if restarted.Current() != 1 {
		t.Fatal("manual rollback state wrong")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	failed := state(occupied.Addr().(*net.TCPAddr).Port, 3)
	if result, err := restarted.Apply(ctx, failed); err == nil || !result.RolledBack {
		t.Fatal("failed new core did not rollback", result, err)
	}
	if result, err := restarted.Apply(ctx, failed); err == nil || !result.RolledBack {
		t.Fatal("failure receipt not preserved", result, err)
	}
	if restarted.Current() != 1 {
		t.Fatal("failure changed successful manifest")
	}
	encrypted, err := os.ReadFile(filepath.Join(directory, "revisions", "1", "desired.json.enc"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if err := os.WriteFile(filepath.Join(directory, "revisions", "1", "desired.json.enc"), encrypted, 0600); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(ctx); err == nil {
		t.Fatal("tampered saved state accepted")
	}
}
