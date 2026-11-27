package reconcile

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestFirstApplyFailureRestoresEmptyRuntime(t *testing.T) {
	hostErr := errors.New("host network failed")
	clearErr := errors.New("host network clear failed")
	for _, tt := range []struct {
		name      string
		failHost  bool
		failClear bool
		code      string
	}{
		{name: "host_network", failHost: true, code: hostNetworkFailedCode},
		{name: "persistence", code: "persistence_failed"},
		{name: "cleanup", failHost: true, failClear: true, code: "rollback_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			adapter := &fakeCoreAdapter{}
			manager, err := New(directory, "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			var cleared bool
			manager.HostNetworkSync = func(_ context.Context, network desiredstate.Network) error {
				if network.HostSourceIPv4 == "" {
					cleared = true
					if tt.failClear {
						return clearErr
					}
					return nil
				}
				if tt.failHost {
					return hostErr
				}
				// Block the original success receipt after the runtime switch.
				return os.Mkdir(manager.successReceiptPath(1), 0700)
			}
			configuration := state(1080, 1)
			configuration.Network = &desiredstate.Network{HostSourceIPv4: "198.51.100.4"}
			result, err := manager.Apply(ctx, configuration)
			if err == nil || result.Success || result.Code != tt.code || result.RolledBack == tt.failClear {
				t.Errorf("unexpected apply result: %+v %v", result, err)
			}
			if tt.failHost && !errors.Is(err, hostErr) || tt.failClear && !errors.Is(err, clearErr) {
				t.Errorf("lost original errors: %v", err)
			}
			if !adapter.closed || !cleared || manager.Current() != 0 {
				t.Errorf("empty runtime not restored: closed=%t cleared=%t revision=%d", adapter.closed, cleared, manager.Current())
			}
			if _, err := os.Stat(filepath.Join(directory, "revisions", "1")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed first revision left on disk: %v", err)
			}
		})
	}
}

func TestEmptyRuntimeKeepsHostRulesWhileCoreStillRuns(t *testing.T) {
	ctx := context.Background()
	hostErr := errors.New("host network failed")
	stopErr := core.ErrCoreStopUnconfirmed
	adapter := &fakeCoreAdapter{closeErr: stopErr}
	manager, err := New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	var cleared bool
	manager.HostNetworkSync = func(_ context.Context, network desiredstate.Network) error {
		if network.HostSourceIPv4 == "" {
			cleared = true
			return nil
		}
		return hostErr
	}
	configuration := state(1080, 1)
	configuration.Network = &desiredstate.Network{HostSourceIPv4: "198.51.100.4"}
	result, err := manager.Apply(ctx, configuration)
	if !errors.Is(err, hostErr) || !errors.Is(err, stopErr) || result.Code != "rollback_failed" || result.RolledBack {
		t.Fatalf("unexpected apply result: %+v %v", result, err)
	}
	if cleared {
		t.Error("host rules were cleared while the core stop was unconfirmed")
	}
}

func TestResetToZeroDoesNotResurrectPreviousCore(t *testing.T) {
	ctx := context.Background()
	adapter := &fakeXrayRuntime{}
	runtime, err := core.NewMultiRuntime(adapter)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(t.TempDir(), "node-test", runtime)
	if err != nil {
		t.Fatal(err)
	}
	first := state(1080, 1)
	first.Network = &desiredstate.Network{HostSourceIPv4: "198.51.100.4"}
	var network desiredstate.Network
	manager.HostNetworkSync = func(_ context.Context, desired desiredstate.Network) error {
		network = desired
		return nil
	}
	if result, err := manager.Apply(ctx, first); err != nil || !result.Success {
		t.Fatalf("first apply: %+v %v", result, err)
	}
	if success, code, err := manager.RollbackCommand(ctx, "reset", 0); err != nil || !success {
		t.Fatalf("reset: %t %s %v", success, code, err)
	}
	if !adapter.closed || runtime.CoreActive("xray") || network != (desiredstate.Network{}) || manager.Current() != 0 {
		t.Errorf("reset left an active runtime or host rules: closed=%t active=%t rules=%+v revision=%d", adapter.closed, runtime.CoreActive("xray"), network, manager.Current())
	}
	adapter.applyCodes = []string{"", "start_failed"}
	result, err := manager.Apply(ctx, state(1081, 2))
	if err == nil || result.Success || !result.RolledBack {
		t.Fatalf("switch failure not compensated: %+v %v", result, err)
	}
	if len(adapter.applied) != 1 {
		t.Fatalf("compensation started a core after reset: %d successful starts", len(adapter.applied))
	}
}

func TestRealResetToZeroPreventsNativeXrayRollback(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for native reset integration")
	}
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &xray.Adapter{NodeID: "node-test", Path: binary, Directory: filepath.Join(directory, "runtime")}
	t.Cleanup(func() {
		if err := adapter.Close(); err != nil {
			t.Error(err)
		}
	})
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	first := state(freePort(t), 1)
	if result, err := manager.Apply(ctx, first); err != nil || !result.Success {
		t.Fatalf("first apply: %+v %v", result, err)
	}
	if success, code, err := manager.RollbackCommand(ctx, "reset", 0); err != nil || !success {
		t.Fatalf("reset: %t %s %v", success, code, err)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	result, err := manager.Apply(ctx, state(occupied.Addr().(*net.TCPAddr).Port, 2))
	if err == nil || result.Code != "start_failed" {
		t.Fatalf("expected native startup failure: %+v %v", result, err)
	}
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort(first.Inbounds[0].Listen, strconv.Itoa(first.Inbounds[0].Port)), time.Second); err == nil {
		conn.Close()
		t.Fatal("native rollback reopened the revoked listener")
	}
}
