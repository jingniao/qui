package server

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealPolicyPublicationToAgentMatchesDraftSimulator(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY for policy publication integration")
	}
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
	proxies := &proxy.Repository{Database: app.Database, Vault: configurationVault}
	policies := &policy.Service{Repository: &policy.Repository{Database: app.Database}, Nodes: nodes, Users: users, Proxies: proxies, Settings: &system.Repository{Database: app.Database}}
	proxyService := &proxy.Service{Repository: proxies, Nodes: nodes, Policies: policies}
	deployments := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	n, token, _, err := nodes.CreateEnrollment(ctx, "Policy Agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	nodes.SetPublicIP(ctx, n.ID, "127.0.0.1")
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Policy entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{n.ID}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.Save(ctx, "", user.Input{Username: "policy_flow_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"socks5"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Policy client", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	inbound, err := proxyService.SaveInbound(ctx, "", proxy.InboundInput{Name: "Scoped SOCKS", NodeID: n.ID, Protocol: "socks5", Listen: "127.0.0.1", Port: port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, InboundIDs: []string{inbound.ID}}); err != nil {
		t.Fatal(err)
	}
	rule, err := policies.Save(ctx, "", policy.Input{Name: "User example block", Priority: 10, Enabled: true, Scope: desiredstate.Scope{UserIDs: []string{u.ID}, GroupIDs: []string{group.ID}}, Match: desiredstate.Match{Suffixes: []string{"blocked.example"}, Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := policies.Simulate(ctx, shared.Request{NodeID: n.ID, InboundID: inbound.ID, UserID: u.ID, Domain: "api.blocked.example", Port: 80, Network: "tcp"})
	if err != nil || !decision.Matched || decision.PolicyID != rule.ID || decision.Action.Type != "Block" {
		t.Fatal("draft simulation incorrect", err)
	}
	state, err := proxyService.Snapshot(ctx, n.ID, users)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Policies) != 1 {
		t.Fatal("published snapshot omitted policy")
	}
	revision, err := deployments.Create(ctx, state, "owner", "user policy")
	if err != nil {
		t.Fatal(err)
	}
	if err := deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.DefaultAgent()
	c.ControlAddress = address
	c.DataDirectory = filepath.Join(directory, "agent")
	c.TLS.ClientCA = caPath
	c.CorePath = binaryPath
	agentCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := control.Enroll(agentCtx, c, token, "policy-test"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- control.Run(agentCtx, c, app.Logger) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("policy Agent stop timed out")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	success := false
	for time.Now().Before(deadline) {
		items, _, err := deployments.List(ctx, 1, 20)
		if err == nil && len(items) > 0 && items[0].Status == "success" {
			success = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !success {
		t.Fatal("policy deployment did not succeed")
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "published-policy-default-origin") }))
	defer origin.Close()
	originPort := origin.Listener.Addr().(*net.TCPAddr).Port
	forward := func(domain string) bool {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		connection.Write([]byte{5, 1, 2})
		reply := make([]byte, 2)
		if _, err := io.ReadFull(connection, reply); err != nil {
			t.Fatal(err)
		}
		credential := device.Credentials[0]
		auth := append([]byte{1, byte(len(credential.Username))}, []byte(credential.Username)...)
		auth = append(auth, byte(len(credential.Secret)))
		auth = append(auth, []byte(credential.Secret)...)
		connection.Write(auth)
		if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
			t.Fatal("policy client auth rejected", err)
		}
		request := append([]byte{5, 1, 0, 3, byte(len(domain))}, []byte(domain)...)
		request = binary.BigEndian.AppendUint16(request, uint16(originPort))
		connection.Write(request)
		reply = make([]byte, 10)
		if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
			return false
		}
		io.WriteString(connection, "GET / HTTP/1.0\r\nHost: "+domain+"\r\n\r\n")
		data, err := io.ReadAll(connection)
		return err == nil && len(data) > 0 && strings.Contains(string(data), "published-policy-default-origin")
	}
	if forward("api.blocked.example") {
		t.Fatal("published user policy allowed blocked domain")
	}
	if !forward("127.0.0.1") {
		t.Fatal("default direct rule no longer forwards")
	}
}
