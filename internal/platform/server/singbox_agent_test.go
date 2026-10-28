package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func TestActualSingboxAgentCapabilitiesPublishAndOfflineContinuity(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for actual sing-box Agent")
	}
	app := testServer(t)
	address, stop := startControl(t, app)
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()
	ctx := context.Background()
	expectedCapabilities, err := singbox.Detect(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: app.Database}
	n, token, _, err := nodes.CreateEnrollment(ctx, "Sing-box Agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ca := filepath.Join(root, "ca.crt")
	if err := os.WriteFile(ca, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.DefaultAgent()
	c.CoreName = "sing-box"
	c.CorePath = binary
	c.DataDirectory = filepath.Join(root, "agent")
	c.ControlAddress = address
	c.TLS.ClientCA = ca
	if err := control.Enroll(ctx, c, token, "singbox-integration"); err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: app.Database, Vault: key}
	u, err := users.Save(ctx, "", user.Input{Username: "sing-user", Status: "active", MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Sing device", Protocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := device.Credentials[0]
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portListener.Addr().(*net.TCPAddr).Port
	portListener.Close()
	state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: u.ID, Enabled: true}}, Credentials: []desiredstate.Credential{{ID: credential.ID, UserID: u.ID, DeviceID: device.ID, Protocol: "http", Username: credential.Username, Secret: credential.Secret}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: port, Enabled: true, CredentialIDs: []string{credential.ID}, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}}
	repository := &deployment.Repository{Database: app.Database, Vault: key}
	revision, err := repository.Create(ctx, state, "owner", "sing-box direct")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	agentCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- control.Run(agentCtx, c, app.Logger) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("sing-box Agent did not stop")
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for {
		current, err := repository.Current(ctx, n.ID)
		node, lookupErr := nodes.Get(ctx, n.ID)
		if err == nil && current == revision.ID && lookupErr == nil && node.Capabilities != nil && node.Capabilities.CoreName == "sing-box" && node.Capabilities.SingboxUserStats == expectedCapabilities.SingboxUserStats {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sing-box Agent did not report and apply its real capability")
		}
		time.Sleep(30 * time.Millisecond)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "real-singbox-agent") }))
	defer origin.Close()
	request := func() {
		proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
		proxyURL.User = url.UserPassword(credential.Username, credential.Secret)
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
		if err != nil {
			t.Fatal("real sing-box Agent request failed", err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || string(data) != "real-singbox-agent" {
			t.Fatal("unexpected forwarded body", string(data), err)
		}
	}
	request()
	if expectedCapabilities.SingboxUserStats {
		// The real Agent samples every five seconds and uploads on its heartbeat.
		// Wait for that existing path before taking the control plane offline.
		deadline := time.Now().Add(35 * time.Second)
		for {
			current, err := users.Get(ctx, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.UsedTraffic > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("qui sing-box traffic did not reach user accounting")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	stop()
	stopped = true
	request()
	current, err := users.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expectedCapabilities.SingboxUserStats && current.UsedTraffic == 0 {
		t.Fatal("measured account traffic was lost")
	}
	if !expectedCapabilities.SingboxUserStats && current.UsedTraffic != 0 {
		t.Fatal("unmeasured account telemetry was fabricated", current.UsedTraffic)
	}
}
