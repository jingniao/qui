package importer

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

// These tests consume exporter bytes, never a hand-maintained equivalent client
// configuration. The gate supplies pinned, digest-verified binaries; nothing is
// downloaded. All targets and credentials are ephemeral loopback fixtures.
func TestRealRealityGeneratedExporterConnections(t *testing.T) {
	xray, sb, mh, home := os.Getenv("XRAY_BINARY"), os.Getenv("SINGBOX_BINARY"), os.Getenv("MIHOMO_BINARY"), os.Getenv("MIHOMO_HOME")
	if xray == "" {
		t.Skip("XRAY_BINARY is required")
	}
	for _, cell := range []struct{ client, transport, mode, flow string }{
		{"sing-box", "tcp", "", "xtls-rprx-vision"},
		{"mihomo", "tcp", "", "xtls-rprx-vision"},
		{"mihomo", "xhttp", "auto", ""},
	} {
		t.Run(cell.client+"/"+cell.transport, func(t *testing.T) {
			if cell.client == "sing-box" && sb == "" {
				t.Skip("SINGBOX_BINARY is required")
			}
			if cell.client == "mihomo" && (mh == "" || home == "") {
				t.Skip("MIHOMO_BINARY and MIHOMO_HOME are required")
			}
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
			target.EnableHTTP2 = true
			target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			target.Config.ErrorLog = log.New(io.Discard, "", 0)
			target.StartTLS()
			defer target.Close()
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			sid := "0123abcd"
			endpoint := proxy.Proxy{Name: "s4-fixture", Protocol: "vless", Server: "127.0.0.1", Port: realityPort(t), Credential: proxy.Credential{Password: realityUUID}, Security: "reality", Flow: cell.flow, TLS: &proxy.TLS{ServerName: "localhost", ClientFingerprint: "chrome"}, Reality: &proxy.RealityOptions{PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: &sid}}
			stream := map[string]any{"network": cell.transport, "security": "reality", "realitySettings": map[string]any{"target": target.Listener.Addr().String(), "serverNames": []string{"localhost"}, "privateKey": base64.RawURLEncoding.EncodeToString(key.Bytes()), "shortIds": []string{sid}}}
			if cell.transport == "xhttp" {
				endpoint.TLS.ALPN = []string{"h2"}
				endpoint.Transport = &proxy.Transport{Type: "xhttp", XHTTP: &proxy.XHTTPOptions{Host: "transport.example.com", Path: "/s4", Mode: cell.mode}}
				stream["xhttpSettings"] = map[string]any{"host": "transport.example.com", "path": "/s4", "mode": cell.mode}
			}
			directory := t.TempDir()
			access := filepath.Join(directory, "access.log")
			server := map[string]any{"log": map[string]any{"loglevel": "warning", "access": access}, "inbounds": []any{map[string]any{"tag": "s4-auth", "listen": "127.0.0.1", "port": endpoint.Port, "protocol": "vless", "settings": map[string]any{"decryption": "none", "clients": []any{map[string]any{"id": realityUUID, "email": "s4-user", "flow": cell.flow}}}, "streamSettings": stream}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
			serverPath := filepath.Join(directory, "server.json")
			realityJSON(t, serverPath, server)
			realityCommand(t, xray, "run", "-test", "-c", serverPath)
			realityProcess(t, xray, "run", "-c", serverPath)
			realityReady(t, endpoint.Port)
			var hits atomic.Int32
			nonce := t.Name() + "-authenticated"
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); io.WriteString(w, nonce) }))
			defer origin.Close()
			for _, valid := range []bool{false, true} {
				label := "wrong-user"
				if valid {
					label = "authenticated"
				}
				t.Run(label, func(t *testing.T) {
					p := endpoint
					if !valid {
						p.Credential.Password = "22222222-2222-4222-8222-222222222222"
					}
					clientPort := realityPort(t)
					clientPath := filepath.Join(t.TempDir(), "client.json")
					if cell.client == "sing-box" {
						out, err := (singbox.Exporter{}).Render(format.Profile{Proxies: []proxy.Proxy{p}})
						if err != nil || len(out.Warnings) != 0 {
							t.Fatalf("export: %v %v", err, out.Warnings)
						}
						var fragment map[string]any
						if err = json.Unmarshal(out.Body, &fragment); err != nil {
							t.Fatal(err)
						}
						fragment["inbounds"] = []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": clientPort}}
						realityJSON(t, clientPath, fragment)
						realityCommand(t, sb, "check", "-c", clientPath)
						realityProcess(t, sb, "run", "-c", clientPath)
					} else {
						template := rules.Default()
						template.Client.MixedPort = clientPort
						template.Groups = []rules.Group{{Name: "S4", Type: "select", Members: []string{rules.MemberAll}, EmptyPolicy: "reject"}}
						template.Rules = []rules.Rule{{ID: "all", Type: rules.RuleMatch, Target: "S4", Enabled: true, Priority: 1}}
						resolved := rules.Expand(template, []proxy.Proxy{p})
						out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{p}, Rules: &resolved})
						if err != nil || len(out.Warnings) != 0 {
							t.Fatalf("export: %v %v", err, out.Warnings)
						}
						if err = os.WriteFile(clientPath, out.Body, 0600); err != nil {
							t.Fatal(err)
						}
						realityCommand(t, mh, "-t", "-d", home, "-f", clientPath)
						realityProcess(t, mh, "-d", home, "-f", clientPath)
					}
					realityReady(t, clientPort)
					transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", clientPort)}), DisableKeepAlives: true}
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
					if !valid {
						if request() || hits.Load() != 0 {
							t.Fatal("wrong user reached origin; possible direct fallback")
						}
						return
					}
					until := time.Now().Add(20 * time.Second)
					for !request() {
						if time.Now().After(until) {
							t.Fatal("exported configuration did not authenticate")
						}
						time.Sleep(100 * time.Millisecond)
					}
					accessLog, _ := os.ReadFile(access)
					if hits.Load() < 1 || !strings.Contains(string(accessLog), "email: s4-user") || !strings.Contains(string(accessLog), strings.TrimPrefix(origin.URL, "http://")) {
						t.Fatal("origin response lacks server authenticated-user evidence")
					}
					t.Logf("generated %s/%s: native checks and authenticated HTTP, origin hits=%d", cell.client, cell.transport, hits.Load())
				})
			}
		})
	}
}

func realityPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
func realityJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func realityCommand(t *testing.T, binary string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput(); err != nil {
		t.Fatalf("native validation: %v\n%s", err, out)
	}
}
func realityProcess(t *testing.T, binary string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	file, err := os.Create(filepath.Join(t.TempDir(), "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = file, file
	if err = cmd.Start(); err != nil {
		cancel()
		file.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		cmd.Wait()
		file.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(file.Name())
			t.Logf("synthetic core log: %s", raw)
		}
	})
}
func realityReady(t *testing.T, port int) {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("core listener did not become ready")
}
