package server

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func TestRealCentralDeploymentAndOfflineProxyContinuity(t *testing.T) {
	path := os.Getenv("XRAY_BINARY")
	if path == "" {
		t.Skip("set XRAY_BINARY for end-to-end deployment")
	}
	app := testServer(t)
	address, stop := startControl(t, app)
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()
	repository := &node.Repository{Database: app.Database}
	n, token, _, err := repository.CreateEnrollment(context.Background(), "deployment-agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}

	agentConfig := config.Agent{Development: true, ControlAddress: address, DataDirectory: filepath.Join(directory, "agent"), TLS: config.TLS{ClientCA: caPath}}
	agentConfig.CorePath = path
	agentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := control.Enroll(agentCtx, agentConfig, token, "integration-test"); err != nil {
		t.Fatal(err)
	}
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()
	go func() {
		for {
			connection, err := origin.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				data := make([]byte, 4)
				if _, err := io.ReadFull(connection, data); err == nil {
					connection.Write(data)
				}
			}()
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	configurationVault, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	deployments := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	revision, err := deployments.Create(agentCtx, state, "owner", "SOCKS5 direct integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := deployments.Deploy(agentCtx, revision.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- control.Run(agentCtx, agentConfig, app.Logger) }()
	deadline := time.Now().Add(10 * time.Second)
	succeeded := false
	for time.Now().Before(deadline) {
		items, _, err := deployments.List(agentCtx, 1, 20)
		if err == nil && len(items) == 1 && items[0].Status == "success" {
			succeeded = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !succeeded {
		t.Fatal("central deployment did not reach success")
	}
	proxyRoundtrip := func() {
		t.Helper()
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		connection.Write([]byte{5, 1, 0})
		reply := make([]byte, 2)
		if _, err := io.ReadFull(connection, reply); err != nil {
			t.Fatal(err)
		}
		target := origin.Addr().(*net.TCPAddr)
		request := []byte{5, 1, 0, 1, 127, 0, 0, 1, byte(target.Port >> 8), byte(target.Port)}
		connection.Write(request)
		reply = make([]byte, 10)
		if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
			t.Fatal("proxy connection failed", err)
		}
		connection.Write([]byte("ping"))
		reply = make([]byte, 4)
		if _, err := io.ReadFull(connection, reply); err != nil || string(reply) != "ping" {
			t.Fatal("forwarding failed", err)
		}
	}
	proxyRoundtrip()
	stop()
	stopped = true
	proxyRoundtrip()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Agent shutdown did not finish")
	}
}
