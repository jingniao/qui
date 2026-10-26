package server

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUsersDevicesGroupsSubscriptionHTTPPermissions(t *testing.T) {
	app := testServer(t)
	call := func(method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.RemoteAddr = "192.0.2.15:123"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	response := call("POST", "/api/v1/auth/setup", `{"token":"`+app.SetupToken()+`","username":"owner","password":"user-test-password-123","platformName":"Test","timezone":"UTC"}`, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	var owner auth.Session
	json.Unmarshal(response.Body.Bytes(), &owner)
	response = call("POST", "/api/v1/node-groups", `{"name":"Test entrances","purpose":"subscription","enabled":true,"subscriptionVisible":true,"nodeIds":[]}`, cookie, owner.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var group node.Group
	json.Unmarshal(response.Body.Bytes(), &group)
	input := user.Input{Username: "alice", DisplayName: "Alice", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"socks5"}, NodeGroupIDs: []string{group.ID}}
	body, _ := json.Marshal(input)
	if response := call("POST", "/api/v1/users", string(body), cookie, ""); response.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	response = call("POST", "/api/v1/users", string(body), cookie, owner.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var u user.User
	json.Unmarshal(response.Body.Bytes(), &u)
	response = call("POST", "/api/v1/users/"+u.ID+"/devices", `{"name":"Laptop","protocols":["socks5"]}`, cookie, owner.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var device user.Device
	json.Unmarshal(response.Body.Bytes(), &device)
	if len(device.Credentials) != 1 || device.Credentials[0].Secret == "" {
		t.Fatal("new credential not returned")
	}
	response = call("GET", "/api/v1/devices/"+device.ID, "", cookie, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), device.Credentials[0].Secret) {
		t.Fatal("default device endpoint exposed secret")
	}
	response = call("POST", "/api/v1/users/"+u.ID+"/subscription-token", `{"deviceId":"`+device.ID+`"}`, cookie, owner.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var token struct {
		Token string
		Path  string
	}
	json.Unmarshal(response.Body.Bytes(), &token)
	response = call("GET", token.Path, "", nil, "")
	if response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("empty authorized node set must return empty URI list", response.Code)
	}
	response = call("GET", "/api/v1/subscriptions", "", cookie, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), token.Token) {
		t.Fatal("subscription list exposed token")
	}
	response = call("POST", "/api/v1/admins", `{"username":"viewer","displayName":"Viewer","password":"user-test-password-123","role":"Viewer"}`, cookie, owner.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	response = call("POST", "/api/v1/auth/login", `{"username":"viewer","password":"user-test-password-123"}`, nil, "")
	viewerCookie := response.Result().Cookies()[0]
	var viewer auth.Session
	json.Unmarshal(response.Body.Bytes(), &viewer)
	for _, path := range []string{"/api/v1/users", "/api/v1/devices", "/api/v1/node-groups", "/api/v1/subscriptions", "/api/v1/users/" + u.ID + "/subscription-preview?deviceId=" + device.ID} {
		if response := call("GET", path, "", viewerCookie, ""); response.Code != 403 {
			t.Fatal("Viewer accessed private endpoint", path)
		}
	}
	response = call("DELETE", "/api/v1/users/"+u.ID+"/subscription-token", "", cookie, owner.CSRFToken)
	if response.Code != 204 {
		t.Fatal(response.Body.String())
	}
	response = call("GET", token.Path, "", nil, "")
	if response.Code != 403 {
		t.Fatal("revoked token accepted")
	}
	response = call("DELETE", "/api/v1/users/"+u.ID, "", cookie, owner.CSRFToken)
	if response.Code != 204 {
		t.Fatal(response.Body.String())
	}
	response = call("GET", "/api/v1/devices/"+device.ID, "", cookie, "")
	if response.Code != 404 {
		t.Fatal("user deletion did not remove device")
	}
}

func TestRealUserCredentialSubscriptionAndRepublishedRevocation(t *testing.T) {
	coreBinary := os.Getenv("XRAY_BINARY")
	if coreBinary == "" {
		t.Skip("set XRAY_BINARY for real user access integration")
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
	users := &user.Repository{Database: app.Database, Vault: configurationVault}
	nodes := &node.Repository{Database: app.Database}
	deployments := &deployment.Repository{Database: app.Database, Vault: configurationVault}
	n, token, _, err := nodes.CreateEnrollment(ctx, "User entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.SetPublicIP(ctx, n.ID, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "User entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{n.ID}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.Save(ctx, "", user.Input{Username: "actual_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"socks5"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Laptop", Protocols: []string{"socks5"}})
	if err != nil {
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
	c.CorePath = coreBinary
	agentCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := control.Enroll(agentCtx, c, token, "user-test"); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	composer := &deployment.Handler{Repository: deployments, Nodes: nodes, Users: users, Proxy: &proxy.Service{Repository: &proxy.Repository{Database: app.Database, Vault: configurationVault}, Nodes: nodes}}
	input := deployment.UserAccessInput{NodeID: n.ID, Summary: "User credential test", Protocol: "socks5", Listen: "0.0.0.0", Port: port}
	state, enabled, err := composer.EnsureUserAccess(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := deployments.CreateWithInboundIntent(ctx, state, enabled, "owner", input.Summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := deployments.Deploy(ctx, revision.ID); err != nil {
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
			t.Error("Agent shutdown timed out")
		}
	}()
	wait := func(id int64) {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			items, _, err := deployments.List(ctx, 1, 100)
			if err == nil {
				for _, item := range items {
					if item.ID == id && item.Status == "success" {
						return
					}
					if item.ID == id && (item.Status == "failed" || item.Status == "rollback") {
						t.Fatalf("deployment failed: %+v", item)
					}
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("revision %d did not succeed", id)
	}
	wait(revision.ID)
	service := &subscription.Service{Repository: &subscription.Repository{Database: app.Database}, Users: users, Nodes: nodes, Published: deployments}
	_, subscriptionToken, err := service.Rotate(ctx, u.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, _, err := service.Content(ctx, subscriptionToken, "uri")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "socks5://") {
		t.Fatal("published user entry did not appear")
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
	authenticate := func(password string) net.Conn {
		t.Helper()
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		connection.Write([]byte{5, 1, 2})
		reply := make([]byte, 2)
		if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 2 {
			connection.Close()
			return nil
		}
		username := device.Credentials[0].Username
		request := append([]byte{1, byte(len(username))}, []byte(username)...)
		request = append(request, byte(len(password)))
		request = append(request, []byte(password)...)
		connection.Write(request)
		if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
			connection.Close()
			return nil
		}
		return connection
	}
	connection := authenticate(device.Credentials[0].Secret)
	if connection == nil {
		t.Fatal("independent user credential rejected")
	}
	targetPort := origin.Addr().(*net.TCPAddr).Port
	request := binary.BigEndian.AppendUint16([]byte{5, 1, 0, 1, 127, 0, 0, 1}, uint16(targetPort))
	connection.Write(request)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		t.Fatal("user connect failed", err)
	}
	connection.Write([]byte("ping"))
	reply = make([]byte, 4)
	if _, err := io.ReadFull(connection, reply); err != nil || string(reply) != "ping" {
		t.Fatal("user forwarding failed", err)
	}
	connection.Close()
	rotated, err := users.RotateCredential(ctx, device.ID, "socks5", false)
	if err != nil {
		t.Fatal(err)
	}
	content, _, err = service.Content(ctx, subscriptionToken, "uri")
	if err != nil || len(content) != 0 {
		t.Fatal("unpublished rotated credential exported", err)
	}
	state, _, err = composer.EnsureUserAccess(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	next, err := deployments.Create(ctx, state, "owner", "Credential rotation")
	if err != nil {
		t.Fatal(err)
	}
	if err := deployments.Deploy(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	wait(next.ID)
	if connection := authenticate(device.Credentials[0].Secret); connection != nil {
		connection.Close()
		t.Fatal("old device credential accepted after publish")
	}
	if connection := authenticate(rotated.Secret); connection == nil {
		t.Fatal("new credential rejected after publish")
	} else {
		connection.Close()
	}
	content, _, err = service.Content(ctx, subscriptionToken, "base64")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(content))
	if err != nil || !strings.Contains(string(decoded), rotated.Secret) {
		t.Fatal("rotated subscription did not use new credential")
	}
	_, err = users.Save(ctx, u.ID, user.Input{Username: u.Username, Status: "disabled", MaxDevices: u.MaxDevices, MaxConnections: u.MaxConnections, AllowedProtocols: u.AllowedProtocols, NodeGroupIDs: u.NodeGroupIDs})
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = composer.EnsureUserAccess(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if state.Inbounds[0].Enabled {
		t.Fatal("disabled users generated an active anonymous inbound")
	}
	closed, err := deployments.Create(ctx, state, "owner", "Disable last user")
	if err != nil {
		t.Fatal(err)
	}
	if err := deployments.Deploy(ctx, closed.ID); err != nil {
		t.Fatal(err)
	}
	wait(closed.ID)
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second); err == nil {
		connection.Close()
		t.Fatal("last disabled user left listener open")
	}
}
