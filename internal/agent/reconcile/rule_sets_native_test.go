package reconcile

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/agent/rulesets"
	"proxycontrol/pkg/contracts/desiredstate"
)

type ruleSetTransport func(*http.Request) (*http.Response, error)

func (f ruleSetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRealRuleSetRefreshRestoresAndRetries(t *testing.T) {
	for _, name := range []string{"xray", "sing-box"} {
		t.Run(name, func(t *testing.T) {
			binary := os.Getenv("XRAY_BINARY")
			if name == "sing-box" {
				binary = os.Getenv("SINGBOX_BINARY")
			}
			if binary == "" {
				t.Skip("real core binary required")
			}
			directory := t.TempDir()
			var good []byte
			var err error
			if name == "xray" {
				geosite := os.Getenv("QUI_GEOSITE_DAT")
				if geosite == "" {
					geosite = filepath.Join(filepath.Dir(binary), "geosite.dat")
				}
				good, err = os.ReadFile(geosite)
			} else {
				source, target := filepath.Join(directory, "cn.json"), filepath.Join(directory, "cn.srs")
				if err := os.WriteFile(source, []byte(`{"version":3,"rules":[{"domain_suffix":["example.cn"]}]}`), 0600); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command(binary, "rule-set", "compile", "--output", target, source).CombinedOutput(); err != nil {
					t.Fatalf("compile: %v %s", err, out)
				}
				good, err = os.ReadFile(target)
			}
			if err != nil {
				t.Fatal(err)
			}
			var body atomic.Value
			body.Store(good)
			var hits atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _, _ = w.Write(body.Load().([]byte)) }))
			defer server.Close()
			address, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			transport := server.Client().Transport
			fetcher := &rulesets.Fetcher{Client: &http.Client{Transport: ruleSetTransport(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				copy.URL.Host = address.Host
				return transport.RoundTrip(copy)
			})}}
			runtime := filepath.Join(directory, "runtime")
			var adapter core.Runtime
			if name == "xray" {
				adapter = &xray.Adapter{Path: binary, Directory: runtime, NodeID: "node-test", RuleSets: fetcher}
			} else {
				adapter = &singbox.Adapter{Path: binary, Directory: runtime, NodeID: "node-test", RuleSets: fetcher}
			}
			defer adapter.Close()
			manager, err := New(directory, "node-test", adapter)
			if err != nil {
				t.Fatal(err)
			}
			set := desiredstate.RuleSet{ID: "cn", Behavior: "domain", SingBoxURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs", XrayURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat", XrayTag: "cn", RefreshSeconds: 86400}
			s := state(freePort(t), 1)
			s.Outbounds = append(s.Outbounds, desiredstate.Outbound{ID: "block", Protocol: "block", Enabled: true})
			s.Policies = []desiredstate.Policy{{ID: "cn", Enabled: true, Priority: 1, Match: desiredstate.Match{RuleSets: []desiredstate.RuleSet{set}}, Action: desiredstate.Action{Type: "Block"}}}
			ctx := context.Background()
			if result, err := manager.Apply(ctx, s); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			fileURL := set.XrayURL
			if name == "sing-box" {
				fileURL = set.SingBoxURL
			}
			file := filepath.Join(rulesets.Dir(runtime), rulesets.FileName(fileURL, name == "sing-box"))
			old := time.Now().Add(-48 * time.Hour)
			if err := os.Chtimes(file, old, old); err != nil {
				t.Fatal(err)
			}
			bad := []byte{0x0a, 0xff, 0xff, 0xff}
			if name == "sing-box" {
				bad = []byte("SRS-damaged")
			}
			body.Store(bad)
			s.Revision = 2
			if result, err := manager.Apply(ctx, s); err == nil || result.Code != "validation_failed" {
				t.Fatal("native check must reject damaged content", result, err)
			}
			if data, err := os.ReadFile(file); err != nil || !bytes.Equal(data, good) {
				t.Fatal("previous native file not restored", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "revisions", "2", "result.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("temporary native failure cached", err)
			}
			if err := adapter.Close(); err != nil {
				t.Fatal(err)
			}
			if err := manager.Restore(ctx); err != nil {
				t.Fatal("old revision cannot restart", err)
			}
			body.Store(good)
			if result, err := manager.Apply(ctx, s); err != nil || !result.Success {
				t.Fatal("same revision retry", result, err)
			}
			if hits.Load() != 3 {
				t.Fatalf("download count %d", hits.Load())
			}
			if _, err := os.Stat(file + ".prev"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("backup left", err)
			}
		})
	}
}
