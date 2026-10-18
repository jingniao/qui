package reconcile

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func userTunnel(port int, username, password string, target *net.TCPAddr) (net.Conn, error) {
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) { connection.Close(); return nil, err }
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	connection.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return fail(err)
	}
	packet := append([]byte{1, byte(len(username))}, []byte(username)...)
	packet = append(packet, byte(len(password)))
	packet = append(packet, []byte(password)...)
	connection.Write(packet)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return fail(err)
	}
	if reply[1] != 0 {
		return fail(io.EOF)
	}
	connection.Write(binary.BigEndian.AppendUint16([]byte{5, 1, 0, 1, 127, 0, 0, 1}, uint16(target.Port)))
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return fail(err)
	}
	if reply[1] != 0 {
		return fail(io.EOF)
	}
	return connection, nil
}

func TestRealExpiryClosesExistingFlowRejectsNewAuthAndSurvivesRestart(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for offline expiration")
	}
	ctx := context.Background()
	base := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(base.Unix())
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binary, Directory: filepath.Join(directory, "runtime"), Now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(freePort(t), 1)
	configuration.Users = []desiredstate.User{{ID: "expired-user", Enabled: true, ExpiresAt: base.Add(time.Minute).Format(time.RFC3339)}, {ID: "other-user", Enabled: true}}
	configuration.Credentials = []desiredstate.Credential{{ID: "expired-device", UserID: "expired-user", DeviceID: "first", Protocol: "socks5", Username: "first", Secret: "first-secret"}, {ID: "other-device", UserID: "other-user", DeviceID: "second", Protocol: "socks5", Username: "second", Secret: "second-secret"}}
	configuration.Inbounds[0].CredentialIDs = []string{"expired-device", "other-device"}
	if result, err := manager.Apply(ctx, configuration); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()
	go func() {
		for {
			connection, err := origin.Accept()
			if err != nil {
				return
			}
			go func() { defer connection.Close(); io.Copy(connection, connection) }()
		}
	}()
	target := origin.Addr().(*net.TCPAddr)
	connection, err := userTunnel(configuration.Inbounds[0].Port, "first", "first-secret", target)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write([]byte("ping"))
	reply := make([]byte, 4)
	if _, err := io.ReadFull(connection, reply); err != nil || string(reply) != "ping" {
		t.Fatal("pre-expiry traffic failed")
	}
	clock.Store(base.Add(time.Minute).Unix())
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatal("expiry did not apply", changed, err)
	}
	connection.SetDeadline(time.Now().Add(time.Second))
	connection.Write([]byte("ping"))
	if _, err := io.ReadFull(connection, reply); err == nil {
		t.Fatal("existing expired connection survived")
	}
	if connection, err := userTunnel(configuration.Inbounds[0].Port, "first", "first-secret", target); err == nil {
		connection.Close()
		t.Fatal("new expired credential accepted")
	}
	other, err := userTunnel(configuration.Inbounds[0].Port, "second", "second-secret", target)
	if err != nil {
		t.Fatal("unexpired user lost access")
	}
	other.Close()
	after, err := os.ReadFile(filepath.Join(directory, "current.json"))
	if err != nil || string(after) != string(manifest) || manager.Current() != 1 {
		t.Fatal("expiry changed approved Revision")
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
	if connection, err := userTunnel(configuration.Inbounds[0].Port, "first", "first-secret", target); err == nil {
		connection.Close()
		t.Fatal("restart resurrected expired credential")
	}
}

func TestRealExpiryOfLastAccountClosesListener(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for last-account expiry")
	}
	ctx := context.Background()
	base := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(base.Unix())
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binary, Directory: filepath.Join(directory, "runtime"), Now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(freePort(t), 1)
	configuration.Users = []desiredstate.User{{ID: "user", Enabled: true, ExpiresAt: base.Add(time.Minute).Format(time.RFC3339)}}
	configuration.Credentials = []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "user", Secret: "secret"}}
	configuration.Inbounds[0].CredentialIDs = []string{"credential"}
	if _, err := manager.Apply(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	clock.Store(base.Add(time.Minute).Unix())
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(configuration.Inbounds[0].Port)), 300*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("expired listener became no-auth")
	}
}

func TestRealFailedExpiryTransitionKeepsOldAuthenticatedCore(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for expiry transition failure")
	}
	ctx := context.Background()
	base := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(base.Unix())
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binary, Directory: filepath.Join(directory, "runtime"), Now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(freePort(t), 1)
	configuration.Users = []desiredstate.User{{ID: "user", Enabled: true, ExpiresAt: base.Add(time.Minute).Format(time.RFC3339)}}
	configuration.Credentials = []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", Username: "user", Secret: "secret"}}
	configuration.Inbounds[0].CredentialIDs = []string{"credential"}
	if _, err := manager.Apply(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	clock.Store(base.Add(time.Minute).Unix())
	adapter.Path = filepath.Join(directory, "missing-core")
	if changed, err := manager.Refresh(ctx); err == nil || changed {
		t.Fatal("failed expiry transition not reported")
	}
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(configuration.Inbounds[0].Port)), 300*time.Millisecond); err != nil {
		t.Fatal("failed transition stopped the running core")
	} else {
		connection.Close()
	}
	adapter.Path = binary
	if err := manager.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(configuration.Inbounds[0].Port)), 300*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("recovery revived expired listener")
	}
}
