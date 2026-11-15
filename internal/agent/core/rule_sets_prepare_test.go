package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/pkg/contracts/desiredstate"
)

type preparingCore struct {
	*fakeMultiCore
	prepare func() (*rulesets.Update, error)
}

func TestRuleSetPreparationFailureDiscardsEarlierCoreDownload(t *testing.T) {
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
	events := []string{}
	want := errors.New("second core download failed")
	a := &preparingCore{fakeMultiCore: &fakeMultiCore{name: "xray", events: &events}, prepare: func() (*rulesets.Update, error) {
		return fetcher.Ensure(context.Background(), directory, []rulesets.Need{{URL: address, SingBox: true}})
	}}
	b := &preparingCore{fakeMultiCore: &fakeMultiCore{name: "sing-box", events: &events}, prepare: func() (*rulesets.Update, error) { return nil, want }}
	m, err := NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	update, err := m.PrepareRuleSets(context.Background(), desiredstate.State{Inbounds: []desiredstate.Inbound{{Core: "xray", Enabled: true}, {Core: "sing-box", Enabled: true}}})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := update.Rollback(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "SRS-old" {
		t.Fatal("earlier core lost old file", err)
	}
	entries, err := os.ReadDir(rulesets.Dir(directory))
	if err != nil || len(entries) != 1 {
		t.Fatal("partial preparation leaked files", entries, err)
	}
}

func (c *preparingCore) PrepareRuleSets(context.Context, desiredstate.State) (*rulesets.Update, error) {
	return c.prepare()
}
func (c *preparingCore) CleanupRuleSets(...desiredstate.State) {}

// Membership can change while downloads run. Preparation must own a snapshot,
// and must neither race on the map nor silently skip the second core.
func TestRuleSetPreparationSnapshotsMembership(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	events := []string{}
	want := errors.New("second core preparation failed")
	a := &preparingCore{fakeMultiCore: &fakeMultiCore{name: "xray", events: &events}, prepare: func() (*rulesets.Update, error) { close(started); <-release; return nil, nil }}
	b := &preparingCore{fakeMultiCore: &fakeMultiCore{name: "sing-box", events: &events}, prepare: func() (*rulesets.Update, error) { return nil, want }}
	m, err := NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	s := desiredstate.State{Inbounds: []desiredstate.Inbound{{Core: "xray", Enabled: true}, {Core: "sing-box", Enabled: true}}}
	done := make(chan error, 1)
	go func() {
		update, err := m.PrepareRuleSets(context.Background(), s)
		done <- errors.Join(err, update.Rollback())
	}()
	<-started
	if err := m.RemoveIdle("sing-box"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, want) {
		t.Fatalf("lost prepared core: %v", err)
	}
}

func TestMultiRestoreFailureStillUndoesSwitchedCores(t *testing.T) {
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

	m, a, b, events := multiFixture(t)
	if _, err := m.Apply(context.Background(), multiConfig(1)); err != nil {
		t.Fatal(err)
	}
	b.failApply = true
	*events = nil
	result, err := m.Apply(update.Context(context.Background()), multiConfig(2))
	if err == nil || !strings.Contains(err.Error(), "start failed") || !strings.Contains(err.Error(), ".prev") || result.Code != "rollback_failed" || !result.RolledBack {
		t.Fatal(result, err)
	}
	if a.revision != 1 || b.revision != 1 || !a.healthy || !b.healthy {
		t.Fatalf("cores not restored: %+v %+v", a, b)
	}
	if want := []string{"validate:xray", "validate:sing-box", "apply:xray", "apply:sing-box", "apply:sing-box", "apply:xray"}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("rollback order: %v want %v", *events, want)
	}
}
