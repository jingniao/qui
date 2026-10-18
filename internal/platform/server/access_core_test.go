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
	"proxycontrol/internal/agent/spool"
	access "proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestRealCoreAccessAccountAggregationPrivacyAndOfflineRestartDelivery(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for real core telemetry pipeline")
	}
	runCoreAccessAccountAggregation(t, binary, "xray")
}

func runCoreAccessAccountAggregation(t *testing.T, binary, coreName string) {
	ctx := context.Background()
	app := testServer(t)
	app.Config.ShutdownTimeout = time.Second
	address, stop := startControl(t, app)
	panelRunning := true
	defer func() {
		if panelRunning {
			stop()
		}
	}()
	nodes := &node.Repository{Database: app.Database}
	n, token, _, err := nodes.CreateEnrollment(ctx, "Real telemetry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: app.Database, Vault: key}
	u, err := users.Save(ctx, "", user.Input{Username: "telemetry_user", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Telemetry device", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := device.Credentials[0]
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: u.ID, Enabled: true}}, Credentials: []desiredstate.Credential{{ID: credential.ID, Protocol: "socks5", UserID: u.ID, DeviceID: device.ID, Username: credential.Username, Secret: credential.Secret}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "socks5", Listen: "127.0.0.1", Port: port, Enabled: true, CredentialIDs: []string{credential.ID}, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}, {ID: "block", Protocol: "block", Enabled: true}}, Policies: []desiredstate.Policy{{ID: "blocked-domain", Enabled: true, Match: desiredstate.Match{Domains: []string{"blocked.example"}, Network: "tcp"}, Action: desiredstate.Action{Type: "Block"}}}}
	deployments := &deployment.Repository{Database: app.Database, Vault: key}
	revision, err := deployments.Create(ctx, state, "owner", "real access collection")
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
	c.CorePath = binary
	c.CoreName = coreName
	c.DataDirectory = filepath.Join(directory, "agent")
	c.TLS.ClientCA = caPath
	if err := control.Enroll(ctx, c, token, "real-access-test"); err != nil {
		t.Fatal(err)
	}
	agentCtx, agentCancel := context.WithCancel(ctx)
	agentDone := make(chan error, 1)
	go func() { agentDone <- control.Run(agentCtx, c, app.Logger) }()
	agentRunning := true
	defer func() {
		if agentRunning {
			agentCancel()
			select {
			case err := <-agentDone:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(8 * time.Second):
				t.Error("access Agent stop timeout")
			}
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		current, err := deployments.Current(ctx, n.ID)
		if err == nil && current == revision.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real access revision did not apply")
		}
		time.Sleep(20 * time.Millisecond)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "PRIVATE-RESPONSE-CONTENT") }))
	defer origin.Close()
	proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(port))
	proxyURL.User = url.UserPassword(credential.Username, credential.Secret)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	allowedRequest := func() {
		t.Helper()
		target := strings.Replace(origin.URL, "127.0.0.1", "localhost", 1) + "/PRIVATE-PATH?token=PRIVATE-QUERY"
		request, _ := http.NewRequest("GET", target, nil)
		request.Header.Set("Cookie", "PRIVATE-COOKIE")
		request.Header.Set("Authorization", "PRIVATE-AUTHORIZATION")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("authenticated access request failed")
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	blockedRequest := func() {
		t.Helper()
		if response, err := client.Get("http://blocked.example/PRIVATE-PATH"); err == nil {
			response.Body.Close()
			t.Fatal("blocked domain passed")
		}
	}
	for index := 0; index < 3; index++ {
		allowedRequest()
	}
	blockedRequest()
	filter := access.Filter{NodeID: n.ID, From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)}
	waitCount := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(75 * time.Second)
		for {
			summary, err := app.accessRepository.Summary(ctx, filter)
			if err == nil && summary.Connections == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("real telemetry count timed out", want, summary, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitCount(4)
	used, err := users.Get(ctx, u.ID)
	if err != nil || used.UsedTraffic <= 0 {
		t.Fatal("native account traffic not applied to user", err)
	}
	usedDevice, err := users.Device(ctx, device.ID)
	if err != nil || usedDevice.UsedTraffic != used.UsedTraffic {
		t.Fatal("device usage not equal to its only user's traffic", err)
	}
	accountSummary, err := app.accessRepository.AccountSummary(ctx, filter)
	if err != nil || accountSummary.UploadBytes <= 0 || accountSummary.DownloadBytes <= 0 {
		t.Fatal("native account totals missing", accountSummary, err)
	}
	items, _, err := app.accessRepository.List(ctx, filter, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.UserID != u.ID || item.DeviceID != device.ID || item.TrafficKnown {
			t.Fatal("identity or unknown traffic attribution incorrect", item)
		}
		if !item.Allowed && item.RuleID != "blocked-domain" {
			t.Fatal("actual rule not reported")
		}
		if item.Domain != "localhost" && item.Domain != "blocked.example" {
			t.Fatal("unexpected access domain")
		}
	}
	stop()
	panelRunning = false
	allowedRequest()
	allowedRequest()
	blockedRequest()
	queue, err := spool.Open(ctx, filepath.Join(c.DataDirectory, "spool"), n.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	deadline = time.Now().Add(45 * time.Second)
	for {
		stats, err := queue.Stats(ctx)
		if err == nil && stats.Rollups > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("offline access not persisted")
		}
		time.Sleep(50 * time.Millisecond)
	}
	agentCancel()
	select {
	case err := <-agentDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("offline Agent did not stop")
	}
	agentRunning = false
	// Restart both ends, preserving identity, queue and approved configuration.
	reopened, err := New(app.Config, app.Database, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("test")}}, app.Logger)
	if err != nil {
		t.Fatal(err)
	}
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcListener, err := net.Listen("tcp", address)
	if err != nil {
		httpListener.Close()
		t.Fatal(err)
	}
	panelCtx, panelCancel := context.WithCancel(ctx)
	panelDone := make(chan error, 1)
	go func() { panelDone <- reopened.Serve(panelCtx, httpListener, grpcListener) }()
	defer func() {
		panelCancel()
		select {
		case err := <-panelDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("restarted panel stop timeout")
		}
	}()
	agentCtx, agentCancel = context.WithCancel(ctx)
	defer agentCancel()
	agentDone = make(chan error, 1)
	agentRunning = true
	go func() { agentDone <- control.Run(agentCtx, c, app.Logger) }()
	waitCount(7)
	after, err := users.Get(ctx, u.ID)
	if err != nil || after.UsedTraffic <= used.UsedTraffic {
		t.Fatal("offline or new process traffic not counted", after.UsedTraffic, err)
	}
	summary, err := app.accessRepository.Summary(ctx, filter)
	if err != nil || summary.Blocked != 2 || summary.UnknownTrafficConnections != 7 {
		t.Fatal("offline access duplicated or lost", summary, err)
	}
	// Inspect stored metadata. Content markers and the device password must not
	// occur in either domain dimensions or numeric telemetry tables.
	rows, err := app.Database.Query("SELECT dimensions_json FROM access_rollups")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(data, "PRIVATE-") || strings.Contains(data, credential.Secret) {
			t.Fatal("content or credential persisted in access metadata")
		}
	}
}
