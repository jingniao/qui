package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// TestTemporaryApplyFailureRetries proves a start failure is not cached: the
// same revision succeeds on a second attempt, and the failed revision directory
// (which held sealed secrets) is removed.
func TestTemporaryApplyFailureRetries(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{applyCodes: []string{"start_failed"}}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(1080, 1)
	result, err := manager.Apply(ctx, configuration)
	if err == nil || result.Code != "start_failed" {
		t.Fatalf("temporary failure not reported: %+v %v", result, err)
	}
	revisionDir := filepath.Join(directory, "revisions", "1")
	if _, err := os.Stat(revisionDir); !os.IsNotExist(err) {
		t.Fatalf("failed revision directory was not removed: %v", err)
	}
	result, err = manager.Apply(ctx, configuration)
	if err != nil || !result.Success {
		t.Fatalf("retry did not succeed: %+v %v", result, err)
	}
	if adapter.applyCalls != 2 {
		t.Fatalf("apply called %d times", adapter.applyCalls)
	}
}

// TestStoragePreflightFailureCarriesCodeAndMessage proves a failing storage
// preflight is reported with a fixed code and the reason, not an empty result,
// and never reaches the core.
func TestStoragePreflightFailureCarriesCodeAndMessage(t *testing.T) {
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	manager.PrepareStorage = func(context.Context, desiredstate.State) error { return errors.New("history is locked") }
	result, err := manager.Apply(context.Background(), state(1080, 1))
	if err == nil || result.Code != storagePreflightFailedCode || result.Message != "history is locked" || adapter.applyCalls != 0 {
		t.Fatalf("preflight failure not reported: %+v %v", result, err)
	}
}

// TestPermanentValidationFailureIsCached proves a validation failure is cached
// and answered without re-running, leaving only the result receipt.
func TestPermanentValidationFailureIsCached(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{validationErr: &core.ValidationRejectedError{Err: errors.New("validation failed")}}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(1080, 1)
	result, err := manager.Apply(ctx, configuration)
	if err == nil || result.Code != "validation_failed" {
		t.Fatalf("validation failure not reported: %+v %v", result, err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "revisions", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "result.json" {
		t.Fatalf("permanent failure left more than result.json: %v", entries)
	}
	validations := adapter.validateCalls
	if _, err := manager.Apply(ctx, configuration); err == nil {
		t.Fatal("cached validation failure was not reported")
	}
	if adapter.validateCalls != validations {
		t.Fatal("cached validation failure re-ran validation")
	}
}

func TestValidationTimeoutDoesNotCacheFailure(t *testing.T) {
	cases := map[string]*fakeCoreAdapter{
		"preflight":    {validationErr: context.DeadlineExceeded},
		"during_apply": {applyValidationErr: context.DeadlineExceeded},
	}
	for name, adapter := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			manager, err := New(directory, "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			configuration := state(1080, 1)
			result, err := manager.Apply(ctx, configuration)
			if !errors.Is(err, context.DeadlineExceeded) || result.Code != "validation_failed" {
				t.Fatalf("validation timeout not reported: %+v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(directory, "revisions", "1", "result.json")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("validation timeout left a receipt: %v", err)
			}
			adapter.validationErr, adapter.applyValidationErr = nil, nil
			if result, err := manager.Apply(ctx, configuration); err != nil || !result.Success {
				t.Fatalf("same revision could not retry: %+v %v", result, err)
			}
		})
	}
}

// TestRenderFailureIsCached proves a render failure is cached too.
func TestRenderFailureIsCached(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	adapter := &fakeCoreAdapter{failRender: true}
	manager, err := New(directory, "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	configuration := state(1080, 1)
	result, err := manager.Apply(ctx, configuration)
	if err == nil || result.Code != "render_failed" {
		t.Fatalf("render failure not reported: %+v %v", result, err)
	}
	renders := adapter.renderCalls
	if _, err := manager.Apply(ctx, configuration); err == nil {
		t.Fatal("cached render failure was not reported")
	}
	if adapter.renderCalls != renders {
		t.Fatal("cached render failure re-ran render")
	}
}

// A fresh node whose first revision fails keeps its failure receipt; the Agent
// must still restart from that directory and later apply a good revision.
func TestFirstRevisionFailureLeavesARestartableDirectory(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manager, err := New(directory, "node-test", &fakeCoreAdapter{failRender: true})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state(1080, 1)); err == nil || result.Code != "render_failed" {
		t.Fatalf("render failure not reported: %+v %v", result, err)
	}
	restarted, err := New(directory, "node-test", &fakeCoreAdapter{})
	if err != nil {
		t.Fatalf("restart after a failed first revision: %v", err)
	}
	if restarted.Current() != 0 {
		t.Fatal("failed revision became current")
	}
	if result, err := restarted.Apply(ctx, state(1080, 2)); err != nil || !result.Success {
		t.Fatalf("next revision after restart: %+v %v", result, err)
	}
}
