package server

import (
	"context"
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
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"testing"
	"time"
)

func TestActualQUICAgentPublishedDeviceSubscriptionAndNativeClient(t *testing.T) {
	binary := os.Getenv("SINGBOX_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_BINARY for full QUIC publish/subscription")
	}
	for _, protocol := range []string{"hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			app := testServer(t)
			address, stop := startControl(t, app)
			defer stop()
			owner := budgetOwner(t, app)
			nodes := &node.Repository{Database: app.Database}
			n, token, _, err := nodes.CreateEnrollment(ctx, "QUIC node", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			ca := filepath.Join(root, "ca.crt")
			if err := os.WriteFile(ca, app.Authority.PEM, 0600); err != nil {
				t.Fatal(err)
			}
			configuration := config.DefaultAgent()
			configuration.CoreName = "sing-box"
			configuration.CorePath = binary
			configuration.ControlAddress = address
			configuration.DataDirectory = filepath.Join(root, "agent")
			configuration.TLS.ClientCA = ca
			if err := control.Enroll(ctx, configuration, token, "quic-publish"); err != nil {
				t.Fatal(err)
			}
			agentCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- control.Run(agentCtx, configuration, app.Logger) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(8 * time.Second):
					t.Error("QUIC Agent did not stop")
				}
			}()
			group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "QUIC entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{n.ID}})
			if err != nil {
				t.Fatal(err)
			}
			key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
			if err != nil {
				t.Fatal(err)
			}
			users := &user.Repository{Database: app.Database, Vault: key}
			u, err := users.Save(ctx, "", user.Input{Username: "quic-user", Status: "active", MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{protocol}, NodeGroupIDs: []string{group.ID}})
			if err != nil {
				t.Fatal(err)
			}
			device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "QUIC device", Protocols: []string{protocol}})
			if err != nil {
				t.Fatal(err)
			}
			packet, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := packet.LocalAddr().(*net.UDPAddr).Port
			packet.Close()
			// The test node's advertised address is the owned loopback endpoint.
			if _, err := app.Database.Exec("UPDATE nodes SET public_ip='127.0.0.1' WHERE id=?", n.ID); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				current, err := nodes.Get(ctx, n.ID)
				if err == nil && current.Capabilities != nil && current.Capabilities.CoreName == "sing-box" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("QUIC native capability missing")
				}
				time.Sleep(20 * time.Millisecond)
			}
			tlsMaterial, err := pki.LoadExisting(app.Config.NodeIdentityDirectory)
			if err != nil {
				t.Fatal(err)
			}
			certificate, err := os.ReadFile(filepath.Join(tlsMaterial.Directory, "server.crt"))
			if err != nil {
				t.Fatal(err)
			}
			private, err := os.ReadFile(filepath.Join(tlsMaterial.Directory, "server.key"))
			if err != nil {
				t.Fatal(err)
			}
			input := proxy.InboundInput{Name: "QUIC user entry", NodeID: n.ID, Protocol: protocol, Listen: "0.0.0.0", Port: port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Transport: "quic", Security: "tls", ServerName: "localhost", CertificatePEM: string(certificate), PrivateKeyPEM: string(private), TrustedCAPEM: string(app.Authority.PEM), QUIC: &desiredstate.QUICSettings{}}}
			response := owner.call("POST", "/api/v1/inbounds", input)
			if response.Code != 201 {
				t.Fatal("QUIC HTTP draft failed", response.Code, response.Body.String())
			}
			response = owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": n.ID, "summary": "QUIC publish"})
			var revision deployment.Revision
			json.Unmarshal(response.Body.Bytes(), &revision)
			if response.Code != 201 {
				t.Fatal(response.Code, response.Body.String())
			}
			if response := owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil); response.Code != 202 {
				t.Fatal(response.Code, response.Body.String())
			}
			deployments := &deployment.Repository{Database: app.Database, Vault: key}
			deadline = time.Now().Add(25 * time.Second)
			for {
				current, err := deployments.Current(ctx, n.ID)
				if err == nil && current == revision.ID {
					break
				}
				if time.Now().After(deadline) {
					var status, code string
					app.Database.QueryRow("SELECT status,result_code FROM deployment_targets WHERE revision=?", revision.ID).Scan(&status, &code)
					persisted, decodeErr := deployments.State(ctx, revision.ID)
					rendered, renderErr := (&singbox.Adapter{Path: binary, Directory: t.TempDir()}).Render(ctx, persisted)
					var nativeErr error
					if renderErr == nil {
						nativeErr = (&singbox.Adapter{Path: binary, Directory: t.TempDir()}).Validate(ctx, rendered)
					}
					t.Fatal("published QUIC state did not apply", status, code, decodeErr, renderErr, nativeErr)
				}
				time.Sleep(30 * time.Millisecond)
			}
			service := &subscription.Service{Repository: &subscription.Repository{Database: app.Database}, Users: users, Nodes: nodes, Published: deployments}
			endpoints, err := service.Endpoints(ctx, u.ID, device.ID)
			if err != nil || len(endpoints) != 1 || endpoints[0].Protocol != protocol {
				t.Fatal("published QUIC device missing from subscription", endpoints, err)
			}
			encoded, err := subscription.Export(endpoints, "sing-box")
			if err != nil {
				t.Fatal(err)
			}
			var exported struct {
				Outbounds []json.RawMessage `json:"outbounds"`
			}
			if err := json.Unmarshal(encoded, &exported); err != nil || len(exported.Outbounds) != 1 {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			clientPort := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			clientDocument := map[string]any{"log": map[string]bool{"disabled": true}, "inbounds": []map[string]any{{"type": "http", "tag": "client", "listen": "127.0.0.1", "listen_port": clientPort}}, "outbounds": exported.Outbounds, "route": map[string]any{"final": endpoints[0].NodeName + "-" + endpoints[0].InboundID + "-" + protocol}}
			data, _ := json.Marshal(clientDocument)
			adapter := &singbox.Adapter{Path: binary, Directory: t.TempDir(), NodeID: "quic-client"}
			defer adapter.Close()
			if result, err := adapter.Apply(ctx, core.RenderedConfig{Revision: 1, Data: data, HealthEndpoints: []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(clientPort))}}); err != nil || !result.Success {
				t.Fatal("native exported client rejected", result, err)
			}
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "published-quic-device") }))
			defer origin.Close()
			proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(clientPort))
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			actual, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get(origin.URL)
			if err != nil {
				t.Fatal("published credential/subscription did not forward", err)
			}
			body, err := io.ReadAll(actual.Body)
			actual.Body.Close()
			if err != nil || string(body) != "published-quic-device" {
				t.Fatal("unexpected published client response", string(body), err)
			}
		})
	}
}
