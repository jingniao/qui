package importer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

func ssPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func ssWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func ssStart(t *testing.T, binary string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	logPath := filepath.Join(t.TempDir(), "process.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		cancel()
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		cmd.Wait()
		log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(logPath)
			t.Logf("synthetic core log: %s", raw)
		}
	})
}

func ssWait(t *testing.T, port int) {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("synthetic core listener did not become ready")
}

func ssNative(t *testing.T, binary string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput(); err != nil {
		t.Fatalf("synthetic core native validation failed: %v\n%s", err, out)
	}
}

// The existing real-core package selector discovers this test through the core
// environment variables. No binary is downloaded here. The gate supplies the
// digest-verified versions pinned in v1.4-s0-evidence.md. Each request needs both
// a unique origin response and a server-side authenticated Shadowsocks log; a
// wrong-key control must fail without hitting the origin. URI-provider cases feed
// the actual canonical text to Mihomo, rather than re-parsing it in the panel.
func TestRealSS2022ImportedAES256Connections(t *testing.T) {
	sb, mh, home := os.Getenv("SINGBOX_BINARY"), os.Getenv("MIHOMO_BINARY"), os.Getenv("MIHOMO_HOME")
	if sb == "" {
		t.Skip("SINGBOX_BINARY is required")
	}
	_, fixtures := ssFixtures(t)
	for _, p := range fixtures {
		for _, client := range []string{"mihomo-yaml", "sing-box", "mihomo-uri"} {
			t.Run(p.Name+"/"+client, func(t *testing.T) {
				if client != "sing-box" && (mh == "" || home == "") {
					t.Skip("MIHOMO_BINARY and MIHOMO_HOME are required")
				}
				ssConnection(t, p, client, sb, mh, home)
			})
		}
	}
}

func ssConnection(t *testing.T, p proxy.Proxy, kind, sb, mh, home string) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "server.log")
	p.Port = ssPort(t)
	parts := strings.Split(p.Credential.Password, ":")
	inbound := map[string]any{"type": "shadowsocks", "tag": "s2-auth", "listen": "127.0.0.1", "listen_port": p.Port, "method": p.Method, "password": parts[0]}
	if len(parts) == 2 {
		inbound["users"] = []any{map[string]any{"name": "s2-user", "password": parts[1]}}
	}
	serverPath := filepath.Join(directory, "server.json")
	ssWriteJSON(t, serverPath, map[string]any{"log": map[string]any{"level": "info", "output": logPath, "timestamp": false}, "inbounds": []any{inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}})
	ssNative(t, sb, "check", "-c", serverPath)
	ssStart(t, sb, "run", "-c", serverPath)
	ssWait(t, p.Port)
	var hits atomic.Int32
	nonce := t.Name() + "-authenticated-origin"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); io.WriteString(w, nonce) }))
	defer origin.Close()
	for _, validKey := range []bool{false, true} {
		label := "wrong-key"
		if validKey {
			label = "valid-key"
		}
		t.Run(label, func(t *testing.T) {
			endpoint := p
			if !validKey {
				bad := strings.Split(endpoint.Credential.Password, ":")
				key, _ := base64.StdEncoding.DecodeString(bad[len(bad)-1])
				key[0] ^= 0xff
				bad[len(bad)-1] = base64.StdEncoding.EncodeToString(key)
				clear(key)
				endpoint.Credential.Password = strings.Join(bad, ":")
			}
			port := ssPort(t)
			clientPath := filepath.Join(t.TempDir(), "client.json")
			if kind == "sing-box" {
				out, err := (singbox.Exporter{}).Render(format.Profile{Proxies: []proxy.Proxy{endpoint}})
				if err != nil {
					t.Fatal(err)
				}
				var fragment map[string]any
				if json.Unmarshal(out.Body, &fragment) != nil {
					t.Fatal("invalid exported JSON")
				}
				fragment["inbounds"] = []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}
				ssWriteJSON(t, clientPath, fragment)
				ssNative(t, sb, "check", "-c", clientPath)
				ssStart(t, sb, "run", "-c", clientPath)
			} else {
				var document map[string]any
				if kind == "mihomo-uri" {
					link, err := uri.Link(endpoint)
					if err != nil {
						t.Fatal(err)
					}
					// Mihomo confines provider paths to its home. Keep the seeded
					// geodata, create an isolated 0700 provider directory beneath it,
					// and remove only that directory when this cell finishes.
					providerDirectory, err := os.MkdirTemp(home, "s2-ss2022-")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.RemoveAll(providerDirectory) })
					providerPath := filepath.Join(providerDirectory, "s2-uri.txt")
					if err := os.WriteFile(providerPath, []byte(link+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					document = map[string]any{"mixed-port": port, "allow-lan": false, "mode": "rule", "log-level": "info", "proxy-providers": map[string]any{"s2": map[string]any{"type": "file", "path": providerPath}}, "proxy-groups": []any{map[string]any{"name": "S2", "type": "select", "use": []string{"s2"}}}, "rules": []string{"MATCH,S2"}}
				} else {
					template := rules.Default()
					template.Client.MixedPort = port
					template.Groups = []rules.Group{{Name: "S2", Type: "select", Members: []string{rules.MemberAll}, EmptyPolicy: "reject"}}
					template.Rules = []rules.Rule{{ID: "all", Type: rules.RuleMatch, Target: "S2", Enabled: true, Priority: 1}}
					resolved := rules.Expand(template, []proxy.Proxy{endpoint})
					out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{endpoint}, Rules: &resolved})
					if err != nil {
						t.Fatal(err)
					}
					if yaml.Unmarshal(out.Body, &document) != nil {
						t.Fatal("invalid exported YAML")
					}
				}
				raw, err := yaml.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(clientPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
				ssNative(t, mh, "-t", "-d", home, "-f", clientPath)
				ssStart(t, mh, "-d", home, "-f", clientPath)
			}
			ssWait(t, port)
			transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Timeout: 2 * time.Second, Transport: transport}
			request := func() bool {
				response, err := client.Get(origin.URL)
				if err != nil {
					return false
				}
				defer response.Body.Close()
				body, _ := io.ReadAll(response.Body)
				return response.StatusCode == http.StatusOK && string(body) == nonce
			}
			if !validKey {
				if request() || hits.Load() != 0 {
					t.Fatal("wrong key reached origin (possible direct bypass)")
				}
				return
			}
			until := time.Now().Add(15 * time.Second)
			for !request() {
				if time.Now().After(until) {
					t.Fatal("valid exported endpoint did not reach authenticated origin")
				}
				time.Sleep(100 * time.Millisecond)
			}
			log, _ := os.ReadFile(logPath)
			if hits.Load() < 1 || !strings.Contains(string(log), "inbound/shadowsocks[s2-auth]") || !strings.Contains(string(log), "inbound connection to "+strings.TrimPrefix(origin.URL, "http://")) {
				t.Fatalf("origin response lacks authenticated SS server evidence:\n%s", log)
			}
			t.Logf("%s %s: native checks + authenticated HTTP, origin hits=%d", p.Name, kind, hits.Load())
		})
	}
}
