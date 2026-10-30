package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	panelaccess "proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealThreeAgentChainBatchTCPUDPAndReverseReset(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY for three-Agent chain and rollback integration")
	}
	runThreeAgentChain(t, binaryPath, "xray")
}

func TestRealSingboxThreeAgentChainBatchTCPUDPAndReverseReset(t *testing.T) {
	binaryPath := os.Getenv("SINGBOX_BINARY")
	if binaryPath == "" || os.Getenv("XRAY_BINARY") == "" {
		t.Skip("set SINGBOX_BINARY and XRAY_BINARY for three-Agent interoperability")
	}
	runThreeAgentChain(t, binaryPath, "sing-box")
}

func runThreeAgentChain(t *testing.T, binaryPath, coreName string) {
	ctx := context.Background()
	app := testServer(t)
	app.Config.ShutdownTimeout = time.Second
	address, stop := startControl(t, app)
	defer stop()
	configurationVault, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: app.Database}
	users := &user.Repository{Database: app.Database, Vault: configurationVault}
	proxyService := &proxy.Service{Repository: &proxy.Repository{Database: app.Database, Vault: configurationVault}, Nodes: nodes}
	deployments := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	nodeIDs, tokens := []string{}, []string{}
	for _, name := range []string{"Entry A", "Middle B", "Exit C"} {
		n, token, _, err := nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := nodes.SetPublicIP(ctx, n.ID, "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		nodeIDs, tokens = append(nodeIDs, n.ID), append(tokens, token)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	cancels, results := []context.CancelFunc{}, []chan error{}
	agentConfigs := []config.Agent{}
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
		for _, done := range results {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("chain Agent shutdown timed out")
			}
		}
	}()
	for index, token := range tokens {
		c := config.DefaultAgent()
		c.ControlAddress, c.CorePath = address, binaryPath
		c.CoreName = coreName
		c.DataDirectory = filepath.Join(directory, "agent-"+strconv.Itoa(index))
		c.TLS.ClientCA = caPath
		agentConfigs = append(agentConfigs, c)
		agentCtx, cancel := context.WithCancel(ctx)
		cancels = append(cancels, cancel)
		if err := control.Enroll(agentCtx, c, token, "chain-test"); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		results = append(results, done)
		go func() { done <- control.Run(agentCtx, c, app.Logger) }()
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		ready := true
		for _, nodeID := range nodeIDs {
			n, err := nodes.Get(ctx, nodeID)
			ready = ready && err == nil && len(n.Capabilities.Protocols) > 0
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent capabilities were not reported")
		}
		time.Sleep(20 * time.Millisecond)
	}
	port := func() int {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		value := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		return value
	}
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: nodeIDs[:1]})
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.Save(ctx, "", user.Input{Username: "chain_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Client", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	exit, err := proxyService.SaveInbound(ctx, "", proxy.InboundInput{Name: "C internal Shadowsocks", NodeID: nodeIDs[2], Protocol: "shadowsocks", Listen: "127.0.0.1", Port: port(), Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	middle, err := proxyService.SaveInbound(ctx, "", proxy.InboundInput{Name: "B internal Trojan TLS", NodeID: nodeIDs[1], Protocol: "trojan", Listen: "127.0.0.1", Port: port(), Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := proxyService.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "B to C", NodeID: nodeIDs[1], Protocol: "shadowsocks", Enabled: true, TargetInboundID: exit.ID, SupportTCP: true, SupportUDP: true, FailurePolicy: "Direct"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := proxyService.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "A to B", NodeID: nodeIDs[0], Protocol: "trojan", Enabled: true, TargetInboundID: middle.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := proxyService.SaveChain(ctx, "", proxy.ChainInput{Name: "A B C", NodeIDs: nodeIDs, OutboundIDs: []string{first.ID, second.ID}, Enabled: true, SupportTCP: true, SupportUDP: true, FinalOutboundID: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	policyService := &policy.Service{Repository: &policy.Repository{Database: app.Database}, Nodes: nodes, Users: users, Proxies: proxyService.Repository, Settings: &system.Repository{Database: app.Database}}
	proxyService.Policies = policyService
	if _, err := policyService.Save(ctx, "", policy.Input{Name: "Loopback test services through chain", Priority: 10, Enabled: true, Match: desiredstate.Match{CIDRs: []string{"127.0.0.1/32"}, Network: "all"}, Action: desiredstate.Action{Type: "ProxyChain", TargetID: chain.ID}}); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "three-agent-chain-origin") }))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)
	failedPrimary, err := proxyService.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "Unavailable primary", NodeID: nodeIDs[0], Protocol: "socks5", Address: "127.0.0.1", Port: port(), Enabled: true, SupportTCP: true, SupportUDP: true, ProbeURL: origin.URL, ProbeInterval: 3})
	if err != nil {
		t.Fatal(err)
	}
	primaryGroup, err := proxyService.SaveGroup(ctx, "", proxy.GroupInput{Name: "Primary service pool", NodeID: nodeIDs[0], Enabled: true, Strategy: "primary", ProbeURL: origin.URL, ProbeInterval: 3, Members: []desiredstate.GroupMember{{OutboundID: failedPrimary.ID, Weight: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyService.Save(ctx, "", policy.Input{Name: "TCP pool with real three-Agent backup chain", Priority: 0, Enabled: true, Match: desiredstate.Match{CIDRs: []string{"127.0.0.1/32"}, Ports: []string{originURL.Port()}, Network: "tcp"}, Action: desiredstate.Action{Type: "OutboundGroup", TargetID: primaryGroup.ID, Failover: "BackupProxyChain", BackupID: chain.ID}}); err != nil {
		t.Fatal(err)
	}
	entry, err := proxyService.SaveInbound(ctx, "", proxy.InboundInput{Name: "User VLESS", NodeID: nodeIDs[0], Protocol: "vless", Listen: "127.0.0.1", Port: port(), Enabled: true, SubscriptionVisible: true, DefaultOutbound: "block"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, InboundIDs: []string{entry.ID}}); err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	setup := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "chain-test-password-123", "platformName": "Test", "timezone": "UTC"}, nil, "")
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(setup.Body.Bytes(), &session)
	response := call("POST", "/api/v1/deployment-batches", map[string]string{"chainId": chain.ID, "summary": "three-Agent test"}, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var batch deployment.Batch
	if err := json.Unmarshal(response.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	for index, item := range batch.Items {
		if item.NodeID != nodeIDs[2-index] {
			t.Fatal("chain was not planned exit first")
		}
		if coreName == "sing-box" {
			state, err := deployments.State(ctx, item.Revision)
			if err != nil {
				t.Fatal(err)
			}
			check := &singbox.Adapter{Path: binaryPath, Directory: t.TempDir()}
			check.ConfigureRuntime(core.RuntimeHooks{EnableStats: true, AccessSink: func(access.Rollup) error { return nil }})
			rendered, err := check.Render(ctx, state)
			if err != nil {
				t.Fatal("actual chain snapshot render", item.Position, err)
			}
			if err := check.Validate(ctx, rendered); err != nil {
				t.Fatal("actual chain snapshot native check", item.Position, err)
			}
		}
	}
	response = call("POST", "/api/v1/deployment-batches/"+batch.ID+"/deploy", nil, cookie, session.CSRFToken)
	if response.Code != 202 {
		t.Fatal(response.Body.String())
	}
	waitBatch := func(want string) {
		t.Helper()
		deadline := time.Now().Add(75 * time.Second)
		for time.Now().Before(deadline) {
			current, err := deployments.Batch(ctx, batch.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status == want {
				return
			}
			if current.Status == "rollback_failed" || want == "success" && current.Status == "rolled_back" {
				t.Fatalf("chain batch failed: %+v", current)
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("chain batch timed out waiting for", want)
	}
	waitBatch("success")
	credential := device.Credentials[0]
	localPort := port()
	clientState := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "client", Logging: desiredstate.Logging{DomainOnly: true}, Credentials: []desiredstate.Credential{{ID: credential.ID, Protocol: "vless", Secret: credential.Secret}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: localPort, Enabled: true, DefaultOutbound: "entry"}}, Outbounds: []desiredstate.Outbound{{ID: "entry", Protocol: "vless", Address: "127.0.0.1", Port: entry.Port, Enabled: true, CredentialID: credential.ID}}}
	clientBinary := binaryPath
	if coreName == "sing-box" {
		clientBinary = os.Getenv("XRAY_BINARY")
	}
	client := &xray.Adapter{Path: clientBinary, Directory: t.TempDir(), NodeID: clientState.NodeID}
	defer client.Close()
	rendered, err := client.Render(ctx, clientState)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	proxyURL, err := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(localPort))
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	result, err := httpClient.Get(origin.URL)
	if err != nil {
		t.Fatal("three-node TCP forwarding failed", err)
	}
	data, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil || string(data) != "three-agent-chain-origin" {
		t.Fatal("three-node TCP payload mismatch", err)
	}
	chainUDP(t, localPort, func() {
		items, _, queryErr := app.accessRepository.List(ctx, panelaccess.Filter{NodeID: nodeIDs[0], From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)}, 1, 20)
		if queryErr == nil {
			for _, item := range items {
				t.Logf("observed chain decision: network=%s allowed=%v rule=%s outbound=%s ip=%s port=%d", item.Network, item.Allowed, item.RuleID, item.OutboundID, item.TargetIP, item.TargetPort)
			}
		}
	})
	// A standalone B->C outbound permits Direct, but this chain's policy is
	// Block. Losing C must not turn the chain into direct egress from B.
	cancels[2]()
	select {
	case err := <-results[2]:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exit Agent did not stop")
	}
	transport.CloseIdleConnections()
	if response, err := httpClient.Get(origin.URL); err == nil {
		response.Body.Close()
		t.Fatal("chain bypassed offline exit using intermediate Direct fallback")
	}
	restartCtx, restartCancel := context.WithCancel(ctx)
	cancels[2] = restartCancel
	restartDone := make(chan error, 1)
	results[2] = restartDone
	go func() { restartDone <- control.Run(restartCtx, agentConfigs[2], app.Logger) }()
	deadline = time.Now().Add(15 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(exit.Port)), 200*time.Millisecond)
		if err == nil {
			connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exit Agent did not restore retained revision")
		}
		time.Sleep(50 * time.Millisecond)
	}
	response = call("POST", "/api/v1/deployment-batches/"+batch.ID+"/rollback", nil, cookie, session.CSRFToken)
	if response.Code != 202 {
		t.Fatal(response.Body.String())
	}
	waitBatch("rolled_back")
	for _, nodeID := range nodeIDs {
		if current, err := deployments.Current(ctx, nodeID); err != nil || current != 0 {
			t.Fatal("first deployment rollback retained configuration", err)
		}
	}
	for _, inbound := range []proxy.Inbound{entry, middle, exit} {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(inbound.Port)), 300*time.Millisecond)
		if err == nil {
			connection.Close()
			t.Fatal("rolled-back first publication left a proxy listener running")
		}
	}
}

func chainUDP(t *testing.T, port int, diagnostics ...func()) {
	t.Helper()
	origin, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()
	go func() {
		buffer := make([]byte, 2048)
		for {
			count, peer, err := origin.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			origin.WriteToUDP(buffer[:count], peer)
		}
	}()
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	connection.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		t.Fatal(err)
	}
	connection.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		t.Fatal("UDP association failed", err)
	}
	udp, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(binary.BigEndian.Uint16(reply[8:10]))})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udp.SetDeadline(time.Now().Add(5 * time.Second))
	packet := binary.BigEndian.AppendUint16([]byte{0, 0, 0, 1, 127, 0, 0, 1}, uint16(origin.LocalAddr().(*net.UDPAddr).Port))
	packet = append(packet, []byte("three-node-udp")...)
	if _, err := udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	count, err := udp.Read(buffer)
	if err != nil || count < 10 || string(buffer[10:count]) != "three-node-udp" {
		for _, diagnostic := range diagnostics {
			diagnostic()
		}
		t.Fatal("three-node UDP forwarding failed", err)
	}
}
