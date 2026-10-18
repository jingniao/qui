package server

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealTwoAgentVlessTrojanDeployment(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY for two-Agent relay integration")
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
	proxyService := &proxy.Service{Repository: &proxy.Repository{Database: app.Database, Vault: configurationVault}, Nodes: nodes}
	deployments := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	a, aToken, _, err := nodes.CreateEnrollment(ctx, "Agent A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, bToken, _, err := nodes.CreateEnrollment(ctx, "Agent B", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	nodes.SetPublicIP(ctx, a.ID, "127.0.0.1")
	nodes.SetPublicIP(ctx, b.ID, "127.0.0.1")
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.Save(ctx, "", user.Input{Username: "relay_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Client", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
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
	target, err := proxyService.SaveInbound(ctx, "", proxy.InboundInput{Name: "Trojan target", NodeID: b.ID, Protocol: "trojan", Listen: "127.0.0.1", Port: port(), Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	outbound, err := proxyService.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "A to B", NodeID: a.ID, Protocol: "trojan", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := proxyService.SaveInbound(ctx, "", proxy.InboundInput{Name: "VLESS user", NodeID: a.ID, Protocol: "vless", Listen: "127.0.0.1", Port: port(), Enabled: true, DefaultOutbound: outbound.ID})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	agentContexts := []context.CancelFunc{}
	results := []chan error{}
	defer func() {
		for _, cancel := range agentContexts {
			cancel()
		}
		for _, done := range results {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(3 * time.Second):
				t.Error("relay Agent shutdown timed out")
			}
		}
	}()
	for index, token := range []string{aToken, bToken} {
		c := config.DefaultAgent()
		c.ControlAddress = address
		c.DataDirectory = filepath.Join(directory, "agent-"+strconv.Itoa(index))
		c.TLS.ClientCA = caPath
		c.CorePath = binaryPath
		agentCtx, cancel := context.WithCancel(ctx)
		agentContexts = append(agentContexts, cancel)
		if err := control.Enroll(agentCtx, c, token, "relay-test"); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		results = append(results, done)
		go func(c config.Agent) { done <- control.Run(agentCtx, c, app.Logger) }(c)
	}
	wait := func(revision int64) {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			items, _, err := deployments.List(ctx, 1, 100)
			if err == nil {
				for _, item := range items {
					if item.ID == revision && item.Status == "success" {
						return
					}
					if item.ID == revision && (item.Status == "failed" || item.Status == "rollback") {
						t.Fatalf("relay deployment failed: %+v", item)
					}
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("relay deployment %d timed out", revision)
	}
	for _, nodeID := range []string{b.ID, a.ID} {
		state, err := proxyService.Snapshot(ctx, nodeID, users)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := deployments.Create(ctx, state, "owner", "relay dependency test")
		if err != nil {
			t.Fatal(err)
		}
		if err := deployments.Deploy(ctx, revision.ID); err != nil {
			t.Fatal(err)
		}
		wait(revision.ID)
	}
	localPort := port()
	credential := device.Credentials[0]
	clientState := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "client", Logging: desiredstate.Logging{DomainOnly: true}, Credentials: []desiredstate.Credential{{ID: credential.ID, Protocol: "vless", Secret: credential.Secret}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: localPort, Enabled: true, DefaultOutbound: "entry"}}, Outbounds: []desiredstate.Outbound{{ID: "entry", Protocol: "vless", Address: "127.0.0.1", Port: entry.Port, Enabled: true, CredentialID: credential.ID}}}
	client := &xray.Adapter{Path: binaryPath, Directory: t.TempDir(), NodeID: clientState.NodeID}
	defer client.Close()
	rendered, err := client.Render(ctx, clientState)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "two-agent-relay-origin") }))
	defer origin.Close()
	targetURL, _ := url.Parse(origin.URL)
	targetPort, _ := strconv.Atoi(targetURL.Port())
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), time.Second)
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
	connection.Write(binary.BigEndian.AppendUint16([]byte{5, 1, 0, 1, 127, 0, 0, 1}, uint16(targetPort)))
	reply = make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		t.Fatal("relay SOCKS connect failed", err)
	}
	io.WriteString(connection, "GET / HTTP/1.0\r\nHost: origin\r\n\r\n")
	response, err := io.ReadAll(connection)
	if err != nil || !strings.Contains(string(response), "two-agent-relay-origin") {
		t.Fatal("two-Agent relay did not forward", err)
	}
}
