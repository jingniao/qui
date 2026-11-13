package rulesets

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

const srsPath = "/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"

func srsBody(extra int) []byte { return append([]byte("SRS"), make([]byte, extra)...) }

// fixture is a TLS server that stands in for the allowed CDN host.
type fixture struct {
	server  *httptest.Server
	fetcher *Fetcher
	address string
	hits    atomic.Int32
	handler atomic.Value
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(srsBody(10)) }))
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.handler.Load().(http.HandlerFunc)(w, r)
	}))
	t.Cleanup(f.server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(f.server.Certificate())
	base, _ := url.Parse(f.server.URL)
	f.address = f.server.URL + srsPath
	f.fetcher = &Fetcher{
		Client:    &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}},
		AllowHost: func(host string) bool { return host == base.Hostname() },
		AllowURL:  func(address string, singBox bool) bool { return strings.HasPrefix(address, f.server.URL+"/") },
	}
	return f
}

func (f *fixture) need() Need {
	return Need{URL: f.address, SingBox: true, RefreshSeconds: 3600, IDs: []string{"geosite-cn"}}
}

func (f *fixture) file(runtime string) string {
	return filepath.Join(Dir(runtime), FileName(f.address, true))
}

func TestEnsureDownloadsAtomicallyWithPrivateModes(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(f.file(runtime))
	if err != nil || info.Size() != 13 {
		t.Fatalf("file: %v %v", info, err)
	}
	entries, _ := os.ReadDir(Dir(runtime))
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
	if directory, _ := os.Stat(Dir(runtime)); directory.Mode().Perm()&0077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("directory mode %v", directory.Mode())
	}
	if f.hits.Load() != 1 {
		t.Fatal("expected one download")
	}
	// A fresh copy is not downloaded again.
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil || f.hits.Load() != 1 {
		t.Fatalf("fresh copy downloaded again: %v %d", err, f.hits.Load())
	}
}

func TestEnsureRefreshesAStaleFile(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(f.file(runtime), old, old)
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(srsBody(99)) }))
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(f.file(runtime)); info.Size() != 102 {
		t.Fatalf("stale file not refreshed, size %d", info.Size())
	}
}

func TestEnsureKeepsAStaleFileWhenTheRefreshFails(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(f.file(runtime), old, old)
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) }))
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil {
		t.Fatal("a failed refresh must keep the cached file:", err)
	}
	if info, _ := os.Stat(f.file(runtime)); info.Size() != 13 {
		t.Fatal("cached file was replaced")
	}
	// An error page that returns 200 must not replace a good copy either.
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) }))
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(f.file(runtime)); info.Size() != 13 {
		t.Fatal("non rule set body replaced the cached file")
	}
}

func TestEnsureFailsWithoutACacheAndWithoutADownload(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) }))
	err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, statErr := os.Stat(f.file(runtime)); statErr == nil {
		t.Fatal("a failed download left a file")
	}
}

func TestEnsureRejectsAnOversizedBody(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("SRS"))
		chunk := make([]byte, 1<<20)
		for i := 0; i <= desiredstate.MaxRuleSetFileBytes>>20; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, err := os.Stat(f.file(runtime)); err == nil {
		t.Fatal("oversized file was kept")
	}
	entries, _ := os.ReadDir(Dir(runtime))
	if len(entries) != 0 {
		t.Fatalf("temporary file left: %v", entries)
	}
}

func TestEnsureRefusesARedirectToADisallowedHost(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	var target atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { target.Add(1); w.Write(srsBody(1)) }))
	defer other.Close()
	// The first hop is allowed, the redirect target (localhost) is not.
	base, _ := url.Parse(f.server.URL)
	f.fetcher.AllowHost = func(host string) bool { return host == base.Hostname() }
	redirected := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	f.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected+srsPath, http.StatusFound)
	}))
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{f.need()}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if target.Load() != 0 {
		t.Fatal("the redirect target was contacted")
	}
}

func TestEnsureRefusesAURLOutsideTheAllowlist(t *testing.T) {
	f, runtime := newFixture(t), t.TempDir()
	f.fetcher = &Fetcher{} // contract allowlist, which only knows the CDN hosts
	need := f.need()
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{need}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if f.hits.Load() != 0 {
		t.Fatal("a URL outside the allowlist was fetched")
	}
	plain := Need{URL: "http://testingcf.jsdelivr.net" + srsPath, SingBox: true, RefreshSeconds: 3600}
	if err := f.fetcher.Ensure(context.Background(), runtime, []Need{plain}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("plain HTTP must be refused, got %v", err)
	}
}

func TestNeedsDeduplicatesSharedXrayFiles(t *testing.T) {
	geosite := "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"
	state := desiredstate.State{NodeID: "n", Policies: []desiredstate.Policy{
		{ID: "a", Enabled: true, Match: desiredstate.Match{RuleSets: []desiredstate.RuleSet{{ID: "one", XrayURL: geosite, SingBoxURL: "s1", RefreshSeconds: 7200}, {ID: "two", XrayURL: geosite, SingBoxURL: "s2", RefreshSeconds: 3600}}}},
		{ID: "off", Enabled: false, Match: desiredstate.Match{RuleSets: []desiredstate.RuleSet{{ID: "three", XrayURL: "x", SingBoxURL: "s3", RefreshSeconds: 3600}}}},
		{ID: "other-node", Enabled: true, Scope: desiredstate.Scope{NodeIDs: []string{"m"}}, Match: desiredstate.Match{RuleSets: []desiredstate.RuleSet{{ID: "four", XrayURL: "y", SingBoxURL: "s4", RefreshSeconds: 3600}}}},
	}}
	xray := Needs("xray", state)
	if len(xray) != 1 || xray[0].URL != geosite || xray[0].RefreshSeconds != 3600 || xray[0].SingBox {
		t.Fatalf("xray needs: %+v", xray)
	}
	if singbox := Needs("sing-box", state); len(singbox) != 2 || !singbox[0].SingBox {
		t.Fatalf("sing-box needs: %+v", singbox)
	}
	if !strings.HasSuffix(FileName(geosite, false), ".dat") || !strings.HasSuffix(FileName(geosite, true), ".srs") || len(FileName(geosite, false)) != 20 {
		t.Fatal("file naming")
	}
}

func TestCleanupKeepsReferencedFilesOnly(t *testing.T) {
	runtime := t.TempDir()
	directory := Dir(runtime)
	os.MkdirAll(directory, 0700)
	keep, drop := "0123456789abcdef.srs", "fedcba9876543210.srs"
	for _, name := range []string{keep, drop, ".tmp-1", "unrelated.txt"} {
		os.WriteFile(filepath.Join(directory, name), []byte("x"), 0600)
	}
	Cleanup(runtime, map[string]bool{keep: true})
	for name, want := range map[string]bool{keep: true, drop: false, ".tmp-1": false, "unrelated.txt": true} {
		if _, err := os.Stat(filepath.Join(directory, name)); (err == nil) != want {
			t.Fatalf("%s present=%v want %v", name, err == nil, want)
		}
	}
}
