package clash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

func freePort(t *testing.T, host string) int {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func serve(t *testing.T, host, body string) int {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", host, err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) })}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr().(*net.TCPAddr).Port
}

func waitPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %d never opened", port)
}

func start(t *testing.T, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, name, args...)
	logPath := filepath.Join(t.TempDir(), filepath.Base(name)+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		command.Wait()
		logFile.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("%s log:\n%s", name, data)
		}
	})
}

// A3.4: the generated mihomo profile routes PROXY through a test node (official
// Xray, SOCKS5 inbound), DIRECT straight to the target and REJECT nowhere. Xray's
// access log tells a proxied request from a direct one. Run by the CI cores job.
func TestMihomoRoutesProxyDirectAndReject(t *testing.T) {
	mihomo, xray, home := os.Getenv("MIHOMO_BINARY"), os.Getenv("XRAY_BINARY"), os.Getenv("MIHOMO_HOME")
	if mihomo == "" || xray == "" || home == "" {
		t.Skip("MIHOMO_BINARY, MIHOMO_HOME and XRAY_BINARY are not set")
	}
	proxied := serve(t, "127.0.0.2", "proxied")
	direct := serve(t, "127.0.0.1", "direct")
	rejected := serve(t, "127.0.0.3", "rejected")

	directory := t.TempDir()
	accessLog := filepath.Join(directory, "access.log")
	socksPort := freePort(t, "127.0.0.1")
	xrayConfig, err := json.Marshal(map[string]any{
		"log":       map[string]any{"access": accessLog, "loglevel": "warning"},
		"inbounds":  []any{map[string]any{"tag": "node", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "password", "accounts": []any{map[string]any{"user": "alice", "pass": "pw"}}, "udp": true}}},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	xrayPath := filepath.Join(directory, "xray.json")
	if err := os.WriteFile(xrayPath, xrayConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	start(t, xray, "run", "-c", xrayPath)
	waitPort(t, socksPort)

	template := rules.Default()
	template.Client.MixedPort = freePort(t, "127.0.0.1")
	template.Client.AllowLAN = false
	template.Rules = []rules.Rule{
		{ID: "proxy", Type: rules.RuleIPCIDR, Payload: "127.0.0.2/32", Target: "PROXY", NoResolve: true, Enabled: true, Priority: 1},
		{ID: "reject", Type: rules.RuleIPCIDR, Payload: "127.0.0.3/32", Target: rules.TargetReject, NoResolve: true, Enabled: true, Priority: 2},
		{ID: "final", Type: rules.RuleMatch, Target: rules.TargetDirect, Enabled: true, Priority: 3},
	}
	template.Groups = []rules.Group{{Name: "PROXY", Type: "select", Members: []string{rules.MemberAll}}}
	node := proxy.Proxy{Origin: proxy.Origin{Source: "published", Ref: "socks", Node: proxy.Ref{ID: "node-1", Name: "test node"}}, Name: "test node · socks5", Server: "127.0.0.1", Port: socksPort, Protocol: "socks5", Credential: proxy.Credential{Username: "alice", Password: "pw"}}
	resolved := rules.Expand(template, []proxy.Proxy{node})
	output, err := Meta{}.Render(format.Profile{Proxies: []proxy.Proxy{node}, Rules: &resolved})
	if err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(directory, "profile.yaml")
	if err := os.WriteFile(profilePath, output.Body, 0o600); err != nil {
		t.Fatal(err)
	}
	start(t, mihomo, "-d", home, "-f", profilePath)
	waitPort(t, template.Client.MixedPort)

	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", template.Client.MixedPort)})}}
	get := func(host string, port int) (string, error) {
		response, err := client.Get(fmt.Sprintf("http://%s:%d/", host, port))
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			return "", fmt.Errorf("status %d", response.StatusCode)
		}
		return string(data), err
	}
	if body, err := get("127.0.0.2", proxied); err != nil || body != "proxied" {
		t.Fatalf("PROXY request: %q %v", body, err)
	}
	if body, err := get("127.0.0.1", direct); err != nil || body != "direct" {
		t.Fatalf("DIRECT request: %q %v", body, err)
	}
	if body, err := get("127.0.0.3", rejected); err == nil {
		t.Fatalf("REJECT request was answered: %q", body)
	}
	time.Sleep(500 * time.Millisecond)
	log, _ := os.ReadFile(accessLog)
	if !strings.Contains(string(log), fmt.Sprintf("127.0.0.2:%d", proxied)) {
		t.Fatalf("the PROXY request did not pass the test node:\n%s", log)
	}
	if strings.Contains(string(log), fmt.Sprintf(":%d", direct)) || strings.Contains(string(log), fmt.Sprintf(":%d", rejected)) {
		t.Fatalf("a DIRECT or REJECT request passed the test node:\n%s", log)
	}
}
