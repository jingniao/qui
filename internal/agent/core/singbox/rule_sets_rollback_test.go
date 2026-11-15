package singbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/rulesets"
	"strings"
	"testing"
)

func TestApplySwitchRestoresRuleSetsBeforePreviousCore(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("SRS-new")) }))
	defer server.Close()
	directory := t.TempDir()
	address := server.URL + "/cn.srs"
	if err := os.MkdirAll(rulesets.Dir(directory), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(rulesets.Dir(directory), rulesets.FileName(address, true))
	if err := os.WriteFile(file, []byte("SRS-old"), 0600); err != nil {
		t.Fatal(err)
	}
	fetcher := &rulesets.Fetcher{Client: server.Client(), AllowHost: func(string) bool { return true }, AllowURL: func(string, bool) bool { return true }}
	update, err := fetcher.Ensure(context.Background(), directory, []rulesets.Need{{URL: address, SingBox: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer update.Rollback()
	if err := update.Activate(); err != nil {
		t.Fatal(err)
	}
	result := core.ApplyResult{Revision: 2}
	startedPrevious := false
	err = applySwitch(update.Context(context.Background()), core.RenderedConfig{Revision: 1}, core.RenderedConfig{Revision: 2}, &result, func() error { return nil }, func(_ context.Context, c core.RenderedConfig) error {
		if c.Revision == 2 {
			return errors.New("startup or health failed")
		}
		startedPrevious = true
		data, err := os.ReadFile(file)
		if err != nil || string(data) != "SRS-old" {
			t.Fatalf("old core restarted before file restoration: %q %v", data, err)
		}
		return nil
	})
	if err == nil || !result.RolledBack || !startedPrevious {
		t.Fatal(result, err)
	}
}

func TestApplySwitchRestoreFailureStillRestartsPreviousCore(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("SRS-new")) }))
	defer server.Close()
	directory := t.TempDir()
	address := server.URL + "/cn.srs"
	if err := os.MkdirAll(rulesets.Dir(directory), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(rulesets.Dir(directory), rulesets.FileName(address, true))
	if err := os.WriteFile(file, []byte("SRS-old"), 0600); err != nil {
		t.Fatal(err)
	}
	fetcher := &rulesets.Fetcher{Client: server.Client(), AllowHost: func(string) bool { return true }, AllowURL: func(string, bool) bool { return true }}
	update, err := fetcher.Ensure(context.Background(), directory, []rulesets.Need{{URL: address, SingBox: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer update.Rollback()
	if err := update.Activate(); err != nil {
		t.Fatal(err)
	}
	// A missing backup makes restoration fail on both Windows and Linux.
	if err := os.Remove(file + ".prev"); err != nil {
		t.Fatal(err)
	}
	startupErr := errors.New("startup or health failed")
	result := core.ApplyResult{Revision: 2}
	startedPrevious := false
	err = applySwitch(update.Context(context.Background()), core.RenderedConfig{Revision: 1}, core.RenderedConfig{Revision: 2}, &result, func() error { return nil }, func(_ context.Context, c core.RenderedConfig) error {
		if c.Revision == 2 {
			return startupErr
		}
		startedPrevious = true
		data, err := os.ReadFile(file)
		if err != nil || string(data) != "SRS-new" {
			t.Fatalf("live rule set unavailable during rollback: %q %v", data, err)
		}
		return nil
	})
	if !errors.Is(err, startupErr) || !strings.Contains(err.Error(), ".prev") || result.Code != "rollback_failed" || !result.RolledBack || !startedPrevious {
		t.Fatal(result, err)
	}
}
