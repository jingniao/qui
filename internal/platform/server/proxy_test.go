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
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProxyHTTPPermissionPortAndRelayMasking(t *testing.T) {
	app := testServer(t)
	call := func(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		request.RemoteAddr = "192.0.2.1:123"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	response := call("POST", "/api/v1/auth/setup", map[string]string{"token": app.SetupToken(), "username": "owner", "password": "proxy-test-password-123", "platformName": "Test", "timezone": "UTC"}, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(response.Body.Bytes(), &session)
	nodes := &node.Repository{Database: app.Database}
	a, _, _, _ := nodes.CreateEnrollment(context.Background(), "A", time.Hour)
	b, _, _, _ := nodes.CreateEnrollment(context.Background(), "B", time.Hour)
	nodes.SetPublicIP(context.Background(), b.ID, "192.0.2.11")
	inbound := proxy.InboundInput{Name: "Internal Trojan", NodeID: b.ID, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"}
	response = call("POST", "/api/v1/inbounds", inbound, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var target proxy.Inbound
	json.Unmarshal(response.Body.Bytes(), &target)
	if strings.Contains(response.Body.String(), "PRIVATE KEY") || target.SubscriptionVisible || !target.HasTLSKey {
		t.Fatal("proxy DTO exposed TLS material")
	}
	if response := call("POST", "/api/v1/inbounds", inbound, cookie, session.CSRFToken); response.Code != 400 {
		t.Fatal("port conflict not rejected")
	}
	outbound := proxy.OutboundInput{Name: "A to B", NodeID: a.ID, Protocol: "trojan", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true}
	response = call("POST", "/api/v1/outbounds", outbound, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(response.Body.String(), "PRIVATE KEY") {
		t.Fatal("relay output exposed private key")
	}
	response = call("POST", "/api/v1/revisions/proxy", map[string]string{"nodeId": a.ID, "summary": "relay source"}, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var revision deployment.Revision
	json.Unmarshal(response.Body.Bytes(), &revision)
	response = call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil, cookie, session.CSRFToken)
	if response.Code != 409 {
		t.Fatal("source relay deployed before target")
	}
	response = call("POST", "/api/v1/admins", map[string]string{"username": "viewer", "displayName": "Viewer", "password": "proxy-test-password-123", "role": "Viewer"}, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	response = call("POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "proxy-test-password-123"}, nil, "")
	viewerCookie := response.Result().Cookies()[0]
	for _, path := range []string{"/api/v1/inbounds", "/api/v1/outbounds", "/api/v1/outbound-groups", "/api/v1/proxy-chains"} {
		if response := call("GET", path, nil, viewerCookie, ""); response.Code != 403 {
			t.Fatal("Viewer accessed proxy configuration")
		}
	}
}
func TestRealStoredProxyVlessTrojanConversion(t *testing.T) {
	binaryPath := os.Getenv("XRAY_BINARY")
	if binaryPath == "" {
		t.Skip("set XRAY_BINARY for stored proxy conversion")
	}
	ctx := context.Background()
	app := testServer(t)
	configurationVault, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: app.Database}
	users := &user.Repository{Database: app.Database, Vault: configurationVault}
	service := &proxy.Service{Repository: &proxy.Repository{Database: app.Database, Vault: configurationVault}, Nodes: nodes}
	deployments := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	a, _, _, _ := nodes.CreateEnrollment(ctx, "A", time.Hour)
	b, _, _, _ := nodes.CreateEnrollment(ctx, "B", time.Hour)
	nodes.SetPublicIP(ctx, a.ID, "127.0.0.1")
	nodes.SetPublicIP(ctx, b.ID, "127.0.0.1")
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.Save(ctx, "", user.Input{Username: "proxy_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
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
		number := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		return number
	}
	target, err := service.SaveInbound(ctx, "", proxy.InboundInput{Name: "Trojan relay", NodeID: b.ID, Protocol: "trojan", Listen: "127.0.0.1", Port: port(), Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	outbound, err := service.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "Trojan next hop", NodeID: a.ID, Protocol: "trojan", Enabled: true, TargetInboundID: target.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := service.SaveInbound(ctx, "", proxy.InboundInput{Name: "VLESS entry", NodeID: a.ID, Protocol: "vless", Listen: "127.0.0.1", Port: port(), Enabled: true, SubscriptionVisible: true, DefaultOutbound: outbound.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, InboundIDs: []string{entry.ID}}); err != nil {
		t.Fatal(err)
	}
	apply := func(state desiredstate.State) *xray.Adapter {
		adapter := &xray.Adapter{Path: binaryPath, Directory: t.TempDir(), NodeID: state.NodeID}
		rendered, err := adapter.Render(ctx, state)
		if err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Apply(ctx, rendered)
		if err != nil || !result.Success {
			t.Fatal(result, err)
		}
		t.Cleanup(func() { adapter.Close() })
		return adapter
	}
	targetState, err := service.Snapshot(ctx, b.ID, users)
	if err != nil {
		t.Fatal(err)
	}
	apply(targetState)
	revision, err := deployments.Create(ctx, targetState, "owner", "target")
	if err != nil {
		t.Fatal(err)
	}
	deployments.Deploy(ctx, revision.ID)
	if err := deployments.Result(ctx, b.ID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	sourceState, err := service.Snapshot(ctx, a.ID, users)
	if err != nil {
		t.Fatal(err)
	}
	apply(sourceState)
	localPort := port()
	credential := device.Credentials[0]
	clientState := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "client", Logging: desiredstate.Logging{DomainOnly: true}, Credentials: []desiredstate.Credential{{ID: credential.ID, Protocol: "vless", Secret: credential.Secret}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: localPort, Enabled: true, DefaultOutbound: "entry"}}, Outbounds: []desiredstate.Outbound{{ID: "entry", Protocol: "vless", Address: "127.0.0.1", Port: entry.Port, Enabled: true, CredentialID: credential.ID}}}
	apply(clientState)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "stored-proxy-conversion") }))
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
		t.Fatal(err)
	}
	io.WriteString(connection, "GET / HTTP/1.0\r\nHost: test\r\n\r\n")
	response, err := io.ReadAll(connection)
	if err != nil || !strings.Contains(string(response), "stored-proxy-conversion") {
		t.Fatal("stored proxy conversion failed", err)
	}
}
