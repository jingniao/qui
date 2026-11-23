package xray

import (
	"context"
	"errors"
	"net"

	"strings"
	"testing"

	"proxycontrol/internal/agent/core"
)

// A failed health check names the branch it failed in, so the supervisor's
// restore log says why the core was restarted. A listener that did not answer is
// a sentinel branch: the supervisor restarts a core only after consecutive
// listener failures, because the dial can fail while the core still serves.
func TestHealthReportsTheFailingBranch(t *testing.T) {
	ctx := context.Background()
	t.Run("listener", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		endpoint := listener.Addr().String()
		listener.Close()
		adapter := &Adapter{Directory: t.TempDir(), current: core.RenderedConfig{HealthEndpoints: []string{endpoint}}}
		_, err = adapter.healthLocked(ctx, &core.ExitObservation{})
		if err == nil || !strings.Contains(err.Error(), endpoint) {
			t.Fatalf("listener failure did not name the endpoint: %v", err)
		}
		// The dial error stays in the chain for the restore log.
		var dial *net.OpError
		if !errors.Is(err, core.ErrListenerUnavailable) || !errors.As(err, &dial) {
			t.Fatalf("listener failure lost its branch or its dial error: %v", err)
		}
	})
	t.Run("exit", func(t *testing.T) {
		exit := core.ExitObservation{Exited: true, Waited: true, Err: errors.New("exit status 2")}

		adapter := &Adapter{Directory: t.TempDir()}
		_, err := adapter.healthLocked(ctx, &exit)
		if err == nil || !strings.Contains(err.Error(), "exit status 2") {
			t.Fatalf("exit failure did not keep the exit error: %v", err)
		}
		if errors.Is(err, core.ErrListenerUnavailable) {
			t.Fatalf("an exited core reported a listener failure: %v", err)
		}
	})
	t.Run("recovery", func(t *testing.T) {
		adapter := &Adapter{Directory: t.TempDir(), gateRecovery: true}
		_, err := adapter.healthLocked(ctx, &core.ExitObservation{})
		if err == nil || !strings.Contains(err.Error(), "routing recovery") {
			t.Fatalf("recovery did not name its branch: %v", err)
		}
		if errors.Is(err, core.ErrListenerUnavailable) {
			t.Fatalf("routing recovery reported a listener failure: %v", err)
		}
	})
}
