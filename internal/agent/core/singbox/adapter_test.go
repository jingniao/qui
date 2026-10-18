package singbox

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
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
	"strings"
	"testing"
	"time"
)

func stateFixture(t *testing.T, protocol string, port int) desiredstate.State {
	t.Helper()
	secret := "singbox-device-password"
	if protocol == "vless" {
		secret = "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"
	}
	settings := desiredstate.ProtocolSettings{}
	if protocol == "trojan" {
		settings = localTLS(t)
	}
	return desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "sing-node", Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: "user", Enabled: true}}, Credentials: []desiredstate.Credential{{ID: "credential", UserID: "user", DeviceID: "device", Protocol: protocol, Username: "sing-user", Secret: secret}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: protocol, Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct", CredentialIDs: []string{"credential"}, Settings: settings}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}}
}

func localTLS(t *testing.T) desiredstate.ProtocolSettings {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return desiredstate.ProtocolSettings{Security: "tls", ServerName: "localhost", CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}
}

func TestRenderPolicyPrecedenceAndContentMinimization(t *testing.T) {
	state := stateFixture(t, "http", 1080)
	state.Policies = []desiredstate.Policy{{ID: "deny", Enabled: true, Priority: 10, Match: desiredstate.Match{Suffixes: []string{"blocked.example"}}, Action: desiredstate.Action{Type: "Block"}}}
	rendered, err := Render(context.Background(), state, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var compiled document
	if err := json.Unmarshal(rendered.Data, &compiled); err != nil {
		t.Fatal(err)
	}
	if !compiled.Log.Disabled || len(compiled.Route.Rules) != 3 || compiled.Route.Rules[0].Action != "reject" || compiled.Route.Rules[1].Action != "resolve" || compiled.Route.Rules[2].Outbound != "direct" {
		t.Fatal("policy precedence/default or content minimization lost", compiled.Route)
	}
}

func TestOfficialSingboxValidatesSupportedProtocolDocuments(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for native validation")
	}
	adapter := &Adapter{Path: binary, Directory: t.TempDir()}
	capabilities, err := adapter.DetectCapabilities(context.Background())
	if err != nil || capabilities.CoreName != "sing-box" {
		t.Fatal(capabilities, err)
	}
	for _, protocol := range []string{"http", "socks5", "shadowsocks", "vless", "trojan"} {
		t.Run(protocol, func(t *testing.T) {
			state := stateFixture(t, protocol, 1080)
			rendered, err := adapter.Render(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.Validate(context.Background(), rendered); err != nil {
				t.Fatal("typed document rejected by official core", err)
			}
		})
	}
	for _, transport := range []string{"ws", "grpc"} {
		t.Run("vless-"+transport, func(t *testing.T) {
			state := stateFixture(t, "vless", 1080)
			state.Inbounds[0].Settings = localTLS(t)
			state.Inbounds[0].Settings.Transport = transport
			if transport == "ws" {
				state.Inbounds[0].Settings.Path = "/proxy"
			} else {
				state.Inbounds[0].Settings.ServiceName = "proxy"
			}
			rendered, err := adapter.Render(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.Validate(context.Background(), rendered); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRealHTTPDirectBlockRollbackAndReconcilerRestart(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for real runtime")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	directory := t.TempDir()
	state := stateFixture(t, "http", port)
	adapter := &Adapter{Path: binary, Directory: filepath.Join(directory, "runtime"), NodeID: state.NodeID}
	defer adapter.Close()
	manager, err := reconcile.New(directory, state.NodeID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(context.Background(), state); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "singbox-real-response") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	proxyURL.User = url.UserPassword("sing-user", state.Credentials[0].Secret)
	request := func() error {
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		response, err := (&http.Client{Transport: transport, Timeout: 2 * time.Second}).Get(origin.URL)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err == nil && string(data) != "singbox-real-response" {
			return errors.New("wrong forwarded response")
		}
		return err
	}
	if err := request(); err != nil {
		t.Fatal("native forwarding failed", err)
	}
	conflict, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bad := state
	bad.Revision = 2
	bad.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	bad.Inbounds[0].Port = conflict.Addr().(*net.TCPAddr).Port
	if result, err := manager.Apply(context.Background(), bad); err == nil || !result.RolledBack {
		conflict.Close()
		t.Fatal("failed native startup did not restore previous process", result, err)
	}
	conflict.Close()
	if err := request(); err != nil {
		t.Fatal("automatic rollback did not resume forwarding", err)
	}
	blocked := state
	blocked.Revision = 3
	blocked.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	blocked.Inbounds[0].DefaultOutbound = "block"
	if result, err := manager.Apply(context.Background(), blocked); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := request(); err == nil {
		t.Fatal("Block fell through to Direct")
	}
	if err := manager.Rollback(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := request(); err != nil {
		t.Fatal("rollback lost direct service", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := &Adapter{Path: binary, Directory: filepath.Join(directory, "runtime"), NodeID: state.NodeID}
	defer restarted.Close()
	restored, err := reconcile.New(directory, "sing-node", restarted)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := request(); err != nil {
		t.Fatal("last successful native configuration not restored", err)
	}
}

// TestRealVlessToDirectForwarding is the plan's "VLESS -> Direct" scenario on the
// official sing-box core: a local client reaches the node's VLESS inbound and
// the node forwards the connection to the origin through its direct outbound. It
// is the sing-box counterpart of the Xray "VLESS to direct" subtest of
// TestRealAllProtocolInboundOutboundAndConversion.
func TestRealVlessToDirectForwarding(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for actual VLESS forwarding")
	}
	ctx := context.Background()
	entryPort, clientPort := freePort(t), freePort(t)
	entry := stateFixture(t, "vless", entryPort)
	client := stateFixture(t, "http", clientPort)
	client.NodeID = "sing-client"
	client.Credentials = append(client.Credentials, desiredstate.Credential{ID: "vless-upstream", Protocol: "vless", Internal: true, Secret: entry.Credentials[0].Secret})
	client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "to-entry", Protocol: "vless", Address: "127.0.0.1", Port: entryPort, Enabled: true, CredentialID: "vless-upstream"})
	client.Inbounds[0].DefaultOutbound = "to-entry"
	for _, state := range []desiredstate.State{entry, client} {
		adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
		t.Cleanup(func() { adapter.Close() })
		rendered, err := adapter.Render(ctx, state)
		if err != nil {
			t.Fatal(state.NodeID, err)
		}
		if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
			t.Fatal(state.NodeID, result, err)
		}
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "vless-direct-response") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(clientPort)))
	proxyURL.User = url.UserPassword("sing-user", client.Credentials[0].Secret)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal("VLESS to direct forwarding failed", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "vless-direct-response" {
		t.Fatal("wrong VLESS to direct response", string(data), err)
	}
}

func TestRealVlessToTrojanRelayWithIndependentUserAndRelayCredentials(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for actual protocol relay")
	}
	ctx := context.Background()
	port := func() int {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		value := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		return value
	}
	exitPort, entryPort, clientPort := port(), port(), port()
	relay := desiredstate.Credential{ID: "relay", Protocol: "trojan", Internal: true, Secret: "independent-relay-password"}
	exitTLS := localTLS(t)
	exit := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "sing-exit", Logging: desiredstate.Logging{DomainOnly: true}, Credentials: []desiredstate.Credential{relay}, Inbounds: []desiredstate.Inbound{{ID: "relay-entry", Protocol: "trojan", Listen: "127.0.0.1", Port: exitPort, Enabled: true, CredentialIDs: []string{relay.ID}, DefaultOutbound: "direct", Settings: exitTLS}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	entry := stateFixture(t, "vless", entryPort)
	entry.Credentials = append(entry.Credentials, relay)
	entry.Inbounds[0].DefaultOutbound = "to-exit"
	entry.Outbounds = append(entry.Outbounds, desiredstate.Outbound{ID: "to-exit", Protocol: "trojan", Address: "127.0.0.1", Port: exitPort, Enabled: true, CredentialID: relay.ID, TargetNodeID: exit.NodeID, Settings: desiredstate.ProtocolSettings{Security: "tls", ServerName: "localhost", TrustedCAPEM: exitTLS.CertificatePEM}})
	client := stateFixture(t, "http", clientPort)
	client.NodeID = "sing-client"
	client.Credentials = append(client.Credentials, desiredstate.Credential{ID: "vless-upstream", Protocol: "vless", Internal: true, Secret: entry.Credentials[0].Secret})
	client.Outbounds = append(client.Outbounds, desiredstate.Outbound{ID: "to-entry", Protocol: "vless", Address: "127.0.0.1", Port: entryPort, Enabled: true, CredentialID: "vless-upstream"})
	client.Inbounds[0].DefaultOutbound = "to-entry"
	events := make(chan access.Rollup, 8)
	for _, state := range []desiredstate.State{exit, entry, client} {
		adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
		if state.NodeID != client.NodeID {
			adapter.ConfigureRuntime(core.RuntimeHooks{AccessSink: func(record access.Rollup) error { events <- record; return nil }})
		}
		t.Cleanup(func() { adapter.Close() })
		rendered, err := adapter.Render(ctx, state)
		if err != nil {
			t.Fatal(state.NodeID, err)
		}
		if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
			t.Fatal(state.NodeID, result, err)
		}
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "protocol-relay-success") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(clientPort)))
	proxyURL.User = url.UserPassword("sing-user", client.Credentials[0].Secret)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(strings.Replace(origin.URL, "127.0.0.1", "localhost", 1))
	if err != nil {
		t.Fatal("protocol conversion relay failed", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "protocol-relay-success" {
		t.Fatal("wrong relay response", string(data), err)
	}
	select {
	case record := <-events:
		if record.NodeID != entry.NodeID || record.UserID != "user" || record.DeviceID != "device" || record.Domain != "localhost" || record.OutboundID != "to-exit" || record.OutboundProtocol != "trojan" || record.EgressKnown || record.FinalEgressNodeID != "" {
			t.Fatal("relay audit guessed exit or lost domain", record)
		}
	case <-time.After(time.Second):
		t.Fatal("entry relay observation missing")
	}
	select {
	case extra := <-events:
		t.Fatal("internal relay duplicated user audit", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRealScopedTCPPolicyBlocksOnlyMatchingAccountAndTimeWindow(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for real policy enforcement")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	state := stateFixture(t, "http", port)
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
	defer adapter.Close()
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	adapter.Now = func() time.Time { return now }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "scoped-policy-response") }))
	defer origin.Close()
	target, _ := url.Parse(origin.URL)
	state.Policies = []desiredstate.Policy{{ID: "account-block", Priority: 10, Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{"user"}}, Match: desiredstate.Match{CIDRs: []string{"127.0.0.1/32"}, Ports: []string{target.Port()}, Network: "tcp", TimeRange: "09:00-11:00"}, Action: desiredstate.Action{Type: "Block"}, Timezone: "UTC"}}
	apply := func() {
		rendered, err := adapter.Render(context.Background(), state)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := adapter.Apply(context.Background(), rendered); err != nil || !result.Success {
			t.Fatal(result, err)
		}
	}
	request := func() bool {
		proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
		proxyURL.User = url.UserPassword("sing-user", state.Credentials[0].Secret)
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		response, err := (&http.Client{Transport: transport, Timeout: 2 * time.Second}).Get(origin.URL)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		return err == nil && string(data) == "scoped-policy-response"
	}
	apply()
	if request() {
		t.Fatal("native matching account/time/IP/port rule did not block")
	}
	state.Revision = 2
	now = now.Add(2 * time.Hour)
	apply()
	if !request() {
		t.Fatal("expired time window remained blocked")
	}
	state.Revision = 3
	now = now.Add(-2 * time.Hour)
	state.Policies[0].Scope.UserIDs = []string{"unrelated-user"}
	apply()
	if !request() {
		t.Fatal("unmatched account rule affected another account")
	}
}
