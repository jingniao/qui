package deployment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/gost"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/pkg/contracts/desiredstate"
)

// No PATH searches or hard-coded executable locations. An unset variable skips
// with its name so ci-real-core-test.sh treats missing coverage as failure.
// A supplied but unusable executable fails when started/validated.
func v14Binary(t *testing.T, variable string) string {
	t.Helper()
	value := os.Getenv(variable)
	if value == "" {
		t.Skip(variable + " is unset")
	}
	return value
}

func TestRealV14CompletionPaths(t *testing.T) {
	// Check at the top level: the CI wrapper associates the reason with the
	// following SKIP line. Nested skips are printed again after a parent's PASS
	// and would lose that association in its awk parser.
	binaries := map[string]string{
		"xray":     v14Binary(t, "XRAY_BINARY"),
		"sing-box": v14Binary(t, "SINGBOX_BINARY"),
	}
	mihomo := v14Binary(t, "MIHOMO_BINARY")
	home := v14Binary(t, "MIHOMO_HOME")
	gostPath := v14Binary(t, "GOST_BINARY")
	for _, primary := range []string{"xray", "sing-box"} {
		for _, path := range v14Paths {
			t.Run(primary+"/"+path, func(t *testing.T) {
				binary := binaries[primary]
				gostBinary := ""
				if strings.HasPrefix(path, "P4") {
					gostBinary = gostPath
				}
				f := newV14Fixture(t, path, primary)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				runtimes := map[string]*core.MultiRuntime{}
				startNode := func(state desiredstate.State) *core.MultiRuntime {
					dir := t.TempDir()
					var adapter core.Runtime = &xray.Adapter{Path: binary, Directory: filepath.Join(dir, "xray"), NodeID: state.NodeID}
					if primary == "sing-box" {
						adapter = &singbox.Adapter{Path: binary, Directory: filepath.Join(dir, "sing-box"), NodeID: state.NodeID}
					}
					components := []core.Runtime{adapter}
					if gostBinary != "" {
						components = append(components, &gost.Adapter{Path: gostBinary, Directory: filepath.Join(dir, "gost"), NodeID: state.NodeID})
					}
					runtime, err := core.NewMultiRuntime(components...)
					v14Must(t, err)
					t.Cleanup(func() { v14Must(t, runtime.Close()) })
					// The captured published state is passed unchanged to the Agent
					// adapters. Inbounds listen on wildcard as on deployed nodes;
					// clients and every inter-node connection use 127.0.0.1.
					config, err := runtime.Render(ctx, state)
					v14Must(t, err)
					v14Must(t, runtime.Validate(ctx, config))
					result, err := runtime.Apply(ctx, config)
					v14Must(t, err)
					if !result.Success {
						t.Fatalf("%s apply: %+v", state.NodeID, result)
					}
					return runtime
				}
				for _, state := range f.states {
					runtimes[state.NodeID] = startNode(state)
				}
				const expected = "qui v1.4 A1a through the selected subscription endpoint"
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, expected) }))
				defer server.Close()
				profilePath := filepath.Join(t.TempDir(), "subscription.yaml")
				v14Must(t, os.WriteFile(profilePath, f.profile, 0600))
				logPath := filepath.Join(t.TempDir(), "mihomo.log")
				logFile, err := os.Create(logPath)
				v14Must(t, err)
				clientCtx, stopClient := context.WithCancel(ctx)
				command := osexec.CommandContext(clientCtx, mihomo, "-d", home, "-f", profilePath)
				// Linux Go's system root loader honors SSL_CERT_FILE. Trust the
				// shared ephemeral PKI without skip-cert-verify or changing TLS
				// fields in the exported subscription.
				for _, env := range os.Environ() {
					if !strings.HasPrefix(env, "SSL_CERT_FILE=") {
						command.Env = append(command.Env, env)
					}
				}
				command.Env = append(command.Env, "SSL_CERT_FILE="+f.ca)
				command.Stdout, command.Stderr = logFile, logFile
				t.Cleanup(func() {
					stopClient()
					if command.Process != nil {
						command.Wait()
					}
					logFile.Close()
					if t.Failed() {
						data, _ := os.ReadFile(logPath)
						t.Logf("mihomo log:\n%s", data)
					}
				})
				v14Must(t, command.Start())
				transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", f.mixedPort)}), DisableKeepAlives: true}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
				fetch := func() (string, error) {
					response, e := client.Get(server.URL)
					if e != nil {
						return "", e
					}
					defer response.Body.Close()
					body, e := io.ReadAll(response.Body)
					if response.StatusCode != http.StatusOK {
						return string(body), fmt.Errorf("HTTP %d", response.StatusCode)
					}
					return string(body), e
				}
				awaitBody := func() {
					deadline := time.Now().Add(20 * time.Second)
					for {
						body, e := fetch()
						if e == nil && body == expected {
							return
						}
						if time.Now().After(deadline) {
							t.Fatalf("%s via %s: body=%q error=%v", path, f.name, body, e)
						}
						time.Sleep(100 * time.Millisecond)
					}
				}
				awaitBody()
				// All simulated hosts share loopback. Positive HTTP alone could
				// pass with a missing relay/exit. Stop each configured node in
				// turn: no fresh request may succeed. Restore its SAME snapshot
				// and require traffic again, without a panel republish.
				for _, state := range f.states {
					v14Must(t, runtimes[state.NodeID].Close())
					if body, e := fetch(); e == nil && body == expected {
						t.Fatalf("traffic bypassed stopped node %s", state.NodeID)
					}
					runtimes[state.NodeID] = startNode(state)
					awaitBody()
				}
				t.Logf("%s: published, unchanged republish, mihomo %q, HTTP body and every-hop outage/restart verified", path, f.name)
			})
		}
	}
}
