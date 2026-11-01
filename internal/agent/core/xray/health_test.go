package xray

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"

	"proxycontrol/internal/agent/core"
)

// A failed health check names the branch it failed in, so the supervisor's
// restore log says why the core was restarted.
func TestHealthReportsTheFailingBranch(t *testing.T) {
	ctx := context.Background()
	t.Run("listener", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		endpoint := listener.Addr().String()
		listener.Close()
		adapter := &Adapter{Directory: t.TempDir(), process: &exec.Cmd{}, current: core.RenderedConfig{HealthEndpoints: []string{endpoint}}}
		if _, err := adapter.Health(ctx); err == nil || !strings.Contains(err.Error(), endpoint) {
			t.Fatalf("listener failure did not name the endpoint: %v", err)
		}
	})
	t.Run("exit", func(t *testing.T) {
		done := make(chan error, 1)
		done <- errors.New("exit status 2")
		adapter := &Adapter{Directory: t.TempDir(), process: &exec.Cmd{}, done: done}
		if _, err := adapter.Health(ctx); err == nil || !strings.Contains(err.Error(), "exit status 2") {
			t.Fatalf("exit failure did not keep the exit error: %v", err)
		}
	})
}
