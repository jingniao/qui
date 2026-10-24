package control

import (
	"errors"
	"fmt"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
)

// A startup restore that could not start the retained revision must keep the
// worker alive, while every other failure stays fatal.
func TestStartupRestoreNonFatalSentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"release recovery", core.ErrReleaseRecovery, true},
		{"release recovery wrapped", fmt.Errorf("repair: %w", core.ErrReleaseRecovery), true},
		{"stop unconfirmed", core.ErrCoreStopUnconfirmed, true},
		{"stop unconfirmed joined", errors.Join(errors.New("stop"), core.ErrCoreStopUnconfirmed), true},
		{"runtime restore wrapped", fmt.Errorf("%w: %w", reconcile.ErrRuntimeRestore, errors.New("render failed")), true},
		{"runtime restore joined", errors.Join(errors.New("render"), reconcile.ErrRuntimeRestore), true},
		{"integrity", errors.New("current revision checksum mismatch"), false},
		{"identity", errors.New("saved revision identity mismatch"), false},
		{"nil", nil, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := startupRestoreNonFatal(test.err); got != test.want {
				t.Fatalf("startupRestoreNonFatal(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}
