package reconcile

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func canForward(t *testing.T, port int, origin *httptest.Server) bool {
	t.Helper()
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(2 * time.Second))
	connection.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return false
	}
	targetPort := origin.Listener.Addr().(*net.TCPAddr).Port
	connection.Write(binary.BigEndian.AppendUint16([]byte{5, 1, 0, 1, 127, 0, 0, 1}, uint16(targetPort)))
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		return false
	}
	io.WriteString(connection, "GET / HTTP/1.0\r\nHost: origin\r\n\r\n")
	data, err := io.ReadAll(connection)
	return err == nil && strings.Contains(string(data), "scheduled-policy-origin")
}
func TestRealScheduledPolicyChangesWithoutChangingRevision(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY for schedule integration")
	}
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC).Unix())
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binaryPath, Directory: filepath.Join(directory, "runtime"), Now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(freePort(t), 1)
	configuration.Outbounds = append(configuration.Outbounds, desiredstate.Outbound{ID: "block", Protocol: "block", Enabled: true})
	configuration.Policies = []desiredstate.Policy{{ID: "business-hours-block", Name: "Time block", Priority: 1, Enabled: true, Timezone: "UTC", Match: desiredstate.Match{TimeRange: "09:00-10:00", Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}
	if result, err := manager.Apply(ctx, configuration); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "scheduled-policy-origin") }))
	defer origin.Close()
	port := configuration.Inbounds[0].Port
	if !canForward(t, port, origin) {
		t.Fatal("inactive time rule blocked traffic")
	}
	clock.Store(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).Unix())
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatal("scheduled active rule did not apply", err)
	}
	if canForward(t, port, origin) {
		t.Fatal("active time rule allowed traffic")
	}
	clock.Store(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC).Unix())
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatal("scheduled end did not apply", err)
	}
	if !canForward(t, port, origin) {
		t.Fatal("time rule did not release traffic after window")
	}
	after, err := os.ReadFile(filepath.Join(directory, "current.json"))
	if err != nil || string(after) != string(manifest) || manager.Current() != 1 {
		t.Fatal("runtime schedule changed immutable revision")
	}
}
func TestRealFailedScheduledTransitionKeepsPermissiveCore(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY for failed schedule integration")
	}
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC).Unix())
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binaryPath, Directory: filepath.Join(directory, "runtime"), Now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }}
	defer adapter.Close()
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(freePort(t), 1)
	configuration.Outbounds = append(configuration.Outbounds, desiredstate.Outbound{ID: "block", Protocol: "block", Enabled: true})
	configuration.Policies = []desiredstate.Policy{{ID: "scheduled-block", Priority: 1, Enabled: true, Timezone: "UTC", Match: desiredstate.Match{TimeRange: "09:00-10:00"}, Action: desiredstate.Action{Type: "Block"}}}
	if _, err := manager.Apply(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "scheduled-policy-origin") }))
	defer origin.Close()
	if !canForward(t, configuration.Inbounds[0].Port, origin) {
		t.Fatal("baseline forward failed")
	}
	clock.Store(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).Unix())
	adapter.Path = filepath.Join(directory, "missing-core")
	if changed, err := manager.Refresh(ctx); err == nil || changed {
		t.Fatal("failed transition did not report failure")
	}
	if !canForward(t, configuration.Inbounds[0].Port, origin) {
		t.Fatal("failed transition stopped the running core")
	}
	adapter.Path = binaryPath
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatal("retry did not apply the active rule", err)
	}
	if canForward(t, configuration.Inbounds[0].Port, origin) {
		t.Fatal("retry bypassed active rule")
	}
}
