package singbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func schedulerBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY to the official core")
	}
	return binary
}

func namedProxy(t *testing.T, name string, delay time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("relay-user:relay-secret")) {
			w.WriteHeader(407)
			return
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		if r.Method == http.MethodConnect {
			conn, buffered, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
			request, err := http.ReadRequest(buffered.Reader)
			if err != nil {
				return
			}
			request.Body.Close()
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: "+strconv.Itoa(len(name))+"\r\n\r\n"+name)
			return
		}
		io.WriteString(w, name)
	}))
	t.Cleanup(server.Close)
	return server
}

func groupBody(port int, target string) (string, error) {
	proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	proxyURL.User = url.UserPassword("sing-user", "singbox-device-password")
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(target)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if response.StatusCode != 200 {
		return "", io.ErrUnexpectedEOF
	}
	return string(data), err
}

func addProxy(state *desiredstate.State, id string, server *httptest.Server) {
	parsed, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	credentialID := "relay-" + id
	state.Credentials = append(state.Credentials, desiredstate.Credential{ID: credentialID, Protocol: "http", Internal: true, Username: "relay-user", Secret: "relay-secret"})
	state.Outbounds = append(state.Outbounds, desiredstate.Outbound{ID: id, Protocol: "http", Address: "127.0.0.1", Port: port, CredentialID: credentialID, Enabled: true})
}

func TestNativeStandardReleaseRendersUpstreamEgressGroup(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY to standard core")
	}
	ctx := context.Background()
	for _, strategy := range []string{"round_robin", "latency"} {
		t.Run(strategy, func(t *testing.T) {
			first := namedProxy(t, "first", 0)
			second := namedProxy(t, "second", 0)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-origin") }))
			defer origin.Close()
			state := stateFixture(t, "http", freePort(t))
			addProxy(&state, "first", first)
			addProxy(&state, "second", second)
			state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "pool", Strategy: strategy, Members: []desiredstate.GroupMember{{OutboundID: "first", Weight: 1}, {OutboundID: "second", Weight: 1}}, ProbeURL: origin.URL, ProbeInterval: 3, Failover: "Block"}}
			state.Inbounds[0].DefaultOutbound = "pool"
			events := make(chan access.Rollup, 8)
			a := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
			a.ConfigureRuntime(core.RuntimeHooks{AccessSink: func(record access.Rollup) error { events <- record; return nil }})
			defer a.Close()
			manager, err := reconcile.New(t.TempDir(), state.NodeID, a)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
				t.Fatal(result, err)
			}
			// The official core must accept the rendered upstream group: a
			// non-primary strategy used to be refused by the fork gate.
			if err := a.Probe(ctx); err != nil {
				t.Fatal(err)
			}
			if changed, err := manager.Refresh(ctx); err != nil || !changed {
				t.Fatal("healthy group not applied", changed, err)
			}
			for index := 0; index < 2; index++ {
				body, err := groupBody(state.Inbounds[0].Port, origin.URL)
				if err != nil || body != "first" && body != "second" {
					t.Fatal("request bypassed the upstream group", body, err)
				}
				select {
				case record := <-events:
					// The core picks the member; the access record must name
					// that member, never the internal group tag.
					if record.OutboundID != body || record.OutboundProtocol != "http" || !record.Allowed {
						t.Fatal("core-selected member not attributed", body, record)
					}
				case <-time.After(time.Second):
					t.Fatal("member attribution missing")
				}
			}
		})
	}
}

func TestNativeNestedFallbackAndPolicyDoesNotInheritDirect(t *testing.T) {
	binary := schedulerBinary(t)
	ctx := context.Background()
	primary := namedProxy(t, "primary", 0)
	backup := namedProxy(t, "backup", 0)
	last := namedProxy(t, "last", 0)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-origin") }))
	defer origin.Close()
	state := stateFixture(t, "http", freePort(t))
	for index, server := range []*httptest.Server{primary, backup, last} {
		id := []string{"primary", "backup", "last"}[index]
		addProxy(&state, id, server)
		state.Outbounds[len(state.Outbounds)-1].ProbeURL = origin.URL
		state.Outbounds[len(state.Outbounds)-1].ProbeInterval = 3
	}
	state.Outbounds[2].FailurePolicy = "BackupOutboundGroup"
	state.Outbounds[2].BackupTargetID = "pool"
	state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "pool", Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: "backup", Weight: 1}}, ProbeURL: origin.URL, ProbeInterval: 3, Failover: "BackupProxyChain", BackupTargetID: "backup-chain"}}
	state.ProxyChains = []desiredstate.ProxyChain{{ID: "backup-chain", NodeIDs: []string{state.NodeID, "exit"}, OutboundIDs: []string{"last"}, Failover: "Block"}}
	state.Inbounds[0].DefaultOutbound = "primary"
	a := &Adapter{Path: binary, Directory: filepath.Join(t.TempDir(), "runtime"), NodeID: state.NodeID}
	defer a.Close()
	manager, err := reconcile.New(t.TempDir(), state.NodeID, a)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := a.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		body, err := groupBody(state.Inbounds[0].Port, origin.URL)
		if err != nil || body != want {
			t.Fatal("wrong nested fallback", want, body, err)
		}
	}
	check("primary")
	primary.Close()
	time.Sleep(3050 * time.Millisecond)
	a.Probe(ctx)
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check("backup")
	backup.Close()
	time.Sleep(3050 * time.Millisecond)
	a.Probe(ctx)
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check("last")
	last.Close()
	if body, err := groupBody(state.Inbounds[0].Port, origin.URL); err == nil {
		t.Fatal("failed last chain did not Block", body)
	}
	// A policy's explicit Block overrides an outbound's independent Direct fallback.
	state.Revision = 2
	state.Outbounds[2].FailurePolicy = "Direct"
	state.Outbounds[2].BackupTargetID = ""
	state.Policies = []desiredstate.Policy{{ID: "fixed-exit", Enabled: true, Match: desiredstate.Match{Network: "tcp"}, Action: desiredstate.Action{Type: "Outbound", TargetID: "primary", Failover: "Block"}}}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if body, err := groupBody(state.Inbounds[0].Port, origin.URL); err == nil {
		t.Fatal("policy inherited independent Direct", body)
	}
}

func TestNativeChangedCredentialsAndProbeURLCannotReuseOldHealth(t *testing.T) {
	binary := schedulerBinary(t)
	ctx := context.Background()
	proxy := namedProxy(t, "pinned-egress", 0)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct-origin") }))
	defer origin.Close()
	state := stateFixture(t, "http", freePort(t))
	addProxy(&state, "upstream", proxy)
	state.OutboundGroups = []desiredstate.OutboundGroup{{ID: "pool", Strategy: "primary", Members: []desiredstate.GroupMember{{OutboundID: "upstream", Weight: 1}}, ProbeURL: origin.URL, ProbeInterval: 3, Failover: "Block"}}
	state.Inbounds[0].DefaultOutbound = "pool"
	a := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
	defer a.Close()
	manager, err := reconcile.New(t.TempDir(), state.NodeID, a)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := a.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	before := a.OutboundHealth()["upstream"]
	if !before.Healthy {
		t.Fatal(before)
	}
	state.Revision++
	state.Credentials[len(state.Credentials)-1].Secret = "new-incorrect-relay-secret"
	rendered, err := a.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal(rendered.Data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Route.Rules[len(doc.Route.Rules)-1].Action != "reject" {
		t.Fatal("changed credentials used prior healthy result")
	}
	if after := a.OutboundHealth()["upstream"]; after != before {
		t.Fatal("read-only preview changed live health")
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if len(a.OutboundHealth()) != 0 {
		t.Fatal("old credential health survived process replacement")
	}
	if body, err := groupBody(state.Inbounds[0].Port, origin.URL); err == nil {
		t.Fatal("changed unverified path stayed open", body)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := a.Probe(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(a.OutboundHealth()) != 0 {
		t.Fatal("cancelled probe fabricated failed health")
	}
	state.Revision++
	state.Credentials[len(state.Credentials)-1].Secret = "relay-secret"
	state.OutboundGroups[0].ProbeURL = origin.URL + "/changed"
	rendered, err = a.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(ctx, state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := a.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if changed, err := manager.Refresh(ctx); err != nil || !changed {
		t.Fatal("new probe URL health not applied", changed, err)
	}
	// Check the approved candidate itself; no cached status from the previous URL.
	rendered, err = a.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := groupBody(state.Inbounds[0].Port, origin.URL); err != nil || body != "pinned-egress" {
		t.Fatal(body, err)
	}
}
