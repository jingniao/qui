package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	access "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
)

// flowFakeCore is a core.Runtime that never starts a process. It records every
// configuration the Agent applies so the end-to-end test can prove the panel's
// rendered state reached the node with the user's credential in it, and reports
// itself healthy so the apply is accepted. It also implements the two optional
// upstream telemetry readers so the test can prove they reach the panel.
type flowFakeCore struct {
	mu      sync.Mutex
	healthy bool
	applied []core.RenderedConfig

	credentialID string
}

const (
	flowOnlineCount  = 2
	flowNodeUpload   = 4096
	flowNodeDownload = 8192
)

func (f *flowFakeCore) Name() string { return "xray" }
func (f *flowFakeCore) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{CoreName: "xray", CoreVersion: "flow-test", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Tcp: true, Udp: true}}}, nil
}
func (f *flowFakeCore) Render(_ context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return core.RenderedConfig{}, err
	}
	f.mu.Lock()
	for _, credential := range state.Credentials {
		if !credential.Internal && credential.UserID != "" {
			f.credentialID = credential.ID
		}
	}
	f.mu.Unlock()
	return core.RenderedConfig{NodeID: state.NodeID, Revision: state.Revision, Data: data}, nil
}
func (f *flowFakeCore) Validate(context.Context, core.RenderedConfig) error { return nil }
func (f *flowFakeCore) Apply(_ context.Context, configuration core.RenderedConfig) (core.ApplyResult, error) {
	f.mu.Lock()
	f.applied = append(f.applied, configuration)
	f.healthy = true
	f.mu.Unlock()
	return core.ApplyResult{Revision: configuration.Revision, Success: true, Code: "success"}, nil
}
func (f *flowFakeCore) Rollback(context.Context, int64) error { return nil }
func (f *flowFakeCore) Health(context.Context) (core.HealthStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return core.HealthStatus{Healthy: f.healthy, CheckedAt: time.Now().UTC()}, nil
}
func (f *flowFakeCore) CollectStats(context.Context) (core.StatsBatch, error) {
	return core.StatsBatch{}, nil
}

// ConnectionSnapshot is the upstream per-user online count the Agent reads from
// the running core (Xray GetStatsOnline). It reports one account while a
// configuration is applied.
func (f *flowFakeCore) ConnectionSnapshot(context.Context) (traffic.ConnectionSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applied) == 0 || f.credentialID == "" {
		return traffic.ConnectionSnapshot{}, errors.New("no applied configuration")
	}
	current := f.applied[len(f.applied)-1]
	return traffic.ConnectionSnapshot{NodeID: current.NodeID, Revision: current.Revision, CoreEpoch: "flow-epoch", ObservedAt: time.Now().UTC(), Scope: traffic.ConnectionScope, Connections: flowOnlineCount, Accounts: []traffic.ConnectionAccount{{CredentialID: f.credentialID, Connections: flowOnlineCount}}}, nil
}

// NodeTrafficTotals is the node total the Agent reads from the core's own
// statistics (sing-box Clash API).
func (f *flowFakeCore) NodeTrafficTotals(context.Context) (uint64, uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applied) == 0 {
		return 0, 0, errors.New("no applied configuration")
	}
	return flowNodeUpload, flowNodeDownload, nil
}
func (f *flowFakeCore) ConfigureRuntime(core.RuntimeHooks)  {}
func (f *flowFakeCore) Close() error                        { return nil }
func (f *flowFakeCore) SampleTraffic(context.Context) error { return nil }
func (f *flowFakeCore) SupportsAccountTelemetry() bool      { return false }
func (f *flowFakeCore) OutboundHealth() map[string]scheduler.Health {
	return nil
}

func (f *flowFakeCore) appliedData() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, 0, len(f.applied))
	for _, configuration := range f.applied {
		result = append(result, string(configuration.Data))
	}
	return result
}

type flowHTTP struct {
	app    *Server
	cookie *http.Cookie
	csrf   string
}

func (c flowHTTP) call(method, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	request.RemoteAddr = "192.0.2.20:1234"
	request.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		request.AddCookie(c.cookie)
	}
	request.Header.Set("X-CSRF-Token", c.csrf)
	response := httptest.NewRecorder()
	c.app.HTTP.Handler.ServeHTTP(response, request)
	return response
}

func flowOwner(t *testing.T, app *Server) flowHTTP {
	t.Helper()
	client := flowHTTP{app: app}
	response := client.call("POST", "/api/v1/auth/setup", map[string]any{"token": app.SetupToken(), "username": "flow_owner", "password": "core-flow-password-123", "platformName": "Core flow", "timezone": "UTC"})
	if response.Code != 201 {
		t.Fatalf("owner setup: %d %s", response.Code, response.Body.String())
	}
	var session auth.Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	client.cookie = response.Result().Cookies()[0]
	client.csrf = session.CSRFToken
	return client
}

func flowWait(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestCoreFlowEnrollDeploySubscriptionTelemetryAndBackup drives the product's
// main flow end to end in one process: first-run setup, node enrollment over
// mTLS, a user subscription, a deployed revision applied by a fake core,
// subscription exports, access and traffic telemetry, and a backup round trip.
// It needs no core binary and is never skipped.
func TestCoreFlowEnrollDeploySubscriptionTelemetryAndBackup(t *testing.T) {
	ctx := context.Background()
	app := testServer(t)
	app.Config.ShutdownTimeout = time.Second
	// A successful deploy queues one user-access reconciliation for its node, so
	// the window is shortened before the first publication: the later quota edit
	// in this flow must not queue behind the deploy's own entry in the default
	// one-minute window.
	app.quotaDeployer.window = 100 * time.Millisecond
	address, stop := startControl(t, app)
	defer stop()

	// Step 1: first-run setup with the one-time token, then Owner login.
	owner := flowOwner(t, app)

	// Step 2: create the node and its one-time enrollment token, then enroll.
	nodes := &node.Repository{Database: app.Database}
	pending, enrollmentToken, _, err := nodes.CreateEnrollment(ctx, "Core Flow Node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.SetPublicIP(ctx, pending.ID, "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	fake := &flowFakeCore{}
	agentConfig := config.DefaultAgent()
	agentConfig.ControlAddress = address
	agentConfig.DataDirectory = filepath.Join(directory, "agent")
	agentConfig.TLS.ClientCA = caPath
	previousRuntime := control.NewRuntime
	control.NewRuntime = func(nodeID, runtimeDirectory string) core.Runtime { return fake }
	t.Cleanup(func() { control.NewRuntime = previousRuntime })
	if err := control.Enroll(ctx, agentConfig, enrollmentToken, "core-flow"); err != nil {
		t.Fatal(err)
	}
	// The Agent's first start creates its configuration key through
	// storage.Ensure; create it here because the spool is opened between runs.
	if _, err := vault.Open(filepath.Join(agentConfig.DataDirectory, "configuration.key")); err != nil {
		t.Fatal(err)
	}

	// Step 3: user, device, subscription node group, VLESS inbound, policies.
	groupResponse := owner.call("POST", "/api/v1/node-groups", node.GroupInput{Name: "Core Flow Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{pending.ID}})
	if groupResponse.Code != 201 {
		t.Fatalf("node group: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	var group node.Group
	if err := json.Unmarshal(groupResponse.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	userResponse := owner.call("POST", "/api/v1/users", user.Input{Username: "flow_user", DisplayName: "Flow User", Status: "active", MaxDevices: 2, MaxConnections: 8, AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
	if userResponse.Code != 201 {
		t.Fatalf("user: %d %s", userResponse.Code, userResponse.Body.String())
	}
	var account user.User
	if err := json.Unmarshal(userResponse.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	deviceResponse := owner.call("POST", "/api/v1/users/"+account.ID+"/devices", map[string]any{"name": "Flow Laptop", "protocols": []string{"vless"}})
	if deviceResponse.Code != 201 {
		t.Fatalf("device: %d %s", deviceResponse.Code, deviceResponse.Body.String())
	}
	var device user.Device
	if err := json.Unmarshal(deviceResponse.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	if len(device.Credentials) != 1 || device.Credentials[0].Secret == "" {
		t.Fatal("device has no VLESS credential")
	}
	credential := device.Credentials[0]
	inboundResponse := owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "Flow VLESS", NodeID: pending.ID, Protocol: "vless", Listen: "0.0.0.0", Port: 24443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if inboundResponse.Code != 201 {
		t.Fatalf("inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	policyResponse := owner.call("POST", "/api/v1/policies", map[string]any{"name": "Flow block", "priority": 10, "enabled": true, "scope": desiredstate.Scope{NodeIDs: []string{pending.ID}}, "match": desiredstate.Match{Domains: []string{"blocked.example"}}, "action": desiredstate.Action{Type: "Block"}})
	if policyResponse.Code != 201 {
		t.Fatalf("policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	// Step 4 (part one): build and deploy the revision.
	revisionResponse := owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": pending.ID, "summary": "Core flow revision"})
	if revisionResponse.Code != 201 {
		t.Fatalf("revision: %d %s", revisionResponse.Code, revisionResponse.Body.String())
	}
	var revision deployment.Revision
	if err := json.Unmarshal(revisionResponse.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	if deployResponse := owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil); deployResponse.Code != 202 {
		t.Fatalf("deploy: %d %s", deployResponse.Code, deployResponse.Body.String())
	}

	var agentLogs strings.Builder
	logger := slog.New(slog.NewTextHandler(&agentLogs, nil))
	// Step 4 (part two): the in-process Agent connects over mTLS and applies the
	// pending revision with the fake core.
	firstCtx, firstCancel := context.WithCancel(ctx)
	firstDone := make(chan error, 1)
	go func() { firstDone <- control.Run(firstCtx, agentConfig, logger) }()
	flowWait(t, "the fake core to apply a configuration containing the user credential", func() bool {
		for _, data := range fake.appliedData() {
			if strings.Contains(data, credential.Secret) {
				return true
			}
		}
		return false
	})
	deployments := &deployment.Repository{Database: app.Database}
	flowWait(t, "the panel to record the deployment as successful", func() bool {
		status, err := deployments.TargetStatus(ctx, pending.ID, revision.ID)
		return err == nil && status == "success"
	})
	flowWait(t, "the node to report online with VLESS capability", func() bool {
		current, err := nodes.Get(ctx, pending.ID)
		if err != nil || current.Status != "online" || current.Capabilities == nil {
			return false
		}
		for _, capability := range current.Capabilities.Protocols {
			if capability.Protocol == "vless" && capability.Inbound {
				return true
			}
		}
		return false
	})
	firstCancel()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Agent run: %v\nagent logs:\n%s", err, agentLogs.String())
	}

	// Steps 6 and 7: the node queues one allowed and one blocked access record
	// plus one traffic sample while offline, then the Agent delivers them on its
	// next run. The revision is already applied, so the panel accepts them.
	queue, err := spool.Open(ctx, filepath.Join(agentConfig.DataDirectory, "spool"), pending.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	allowedRecord := access.Rollup{Bucket: now, NodeID: pending.ID, Revision: revision.ID, UserID: account.ID, DeviceID: device.ID, InboundProtocol: "vless", Domain: "allowed.example", TargetPort: 443, Network: "tcp", Action: "Direct", OutboundID: "direct", OutboundProtocol: "direct", Allowed: true, Connections: 2, UploadBytes: 10, DownloadBytes: 20, TrafficKnown: true}
	blockedRecord := access.Rollup{Bucket: now, NodeID: pending.ID, Revision: revision.ID, UserID: account.ID, DeviceID: device.ID, InboundProtocol: "vless", Domain: "blocked.example", TargetPort: 443, Network: "tcp", Action: "Block", Allowed: false, Connections: 1}
	if _, err := queue.Enqueue(ctx, []access.Rollup{allowedRecord, blockedRecord}); err != nil {
		t.Fatal(err)
	}
	counter := traffic.Counter{CoreEpoch: "coreflow-epoch", Revision: revision.ID, CredentialID: credential.ID, UserID: account.ID, DeviceID: device.ID, Protocol: "vless", UploadBytes: 100, DownloadBytes: 200, ObservedAt: now}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}

	secondCtx, secondCancel := context.WithCancel(ctx)
	secondDone := make(chan error, 1)
	go func() { secondDone <- control.Run(secondCtx, agentConfig, logger) }()
	defer func() {
		secondCancel()
		select {
		case err := <-secondDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Agent stop timeout")
		}
	}()
	flowWait(t, "the allowed and blocked access records to reach the panel", func() bool {
		response := owner.call("GET", "/api/v1/access-records", nil)
		return response.Code == 200 && strings.Contains(response.Body.String(), "allowed.example") && strings.Contains(response.Body.String(), "blocked.example")
	})
	blockedResponse := owner.call("GET", "/api/v1/access-records/blocked", nil)
	if blockedResponse.Code != 200 || !strings.Contains(blockedResponse.Body.String(), "blocked.example") || strings.Contains(blockedResponse.Body.String(), "allowed.example") {
		t.Fatalf("blocked records: %d %s", blockedResponse.Code, blockedResponse.Body.String())
	}
	var used int64
	if err := app.Database.QueryRowContext(ctx, "SELECT used_traffic FROM users WHERE id=?", account.ID).Scan(&used); err != nil || used != 300 {
		t.Fatalf("traffic ingest did not update user usage: %d %v", used, err)
	}

	// Step 4c (Phase 6.3): the Agent reads the core's own upstream telemetry and
	// the panel keeps it. The heartbeat carries the node traffic totals the core
	// reports, and the live online snapshot reaches the panel's connection view
	// for the user whose credential it names.
	flowWait(t, "the core's node traffic totals to reach the panel", func() bool {
		current, err := nodes.Get(ctx, pending.ID)
		return err == nil && current.Heartbeat != nil && current.Heartbeat.UploadBytes == flowNodeUpload && current.Heartbeat.DownloadBytes == flowNodeDownload
	})
	flowWait(t, "the core's online snapshot to reach the user view", func() bool {
		response := owner.call("GET", "/api/v1/users/"+account.ID, nil)
		return response.Code == 200 && strings.Contains(response.Body.String(), `"onlineConnections":2`) && strings.Contains(response.Body.String(), `"overLimit":false`)
	})

	// Step 5: the subscription is generated from the node group in every format.
	tokenResponse := owner.call("POST", "/api/v1/users/"+account.ID+"/subscription-token", map[string]any{"deviceId": device.ID})
	if tokenResponse.Code != 201 {
		t.Fatalf("subscription token: %d %s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var token struct {
		Token string `json:"token"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	uriContent := owner.call("GET", token.Path, nil)
	if uriContent.Code != 200 || !strings.Contains(uriContent.Body.String(), "vless://") || !strings.Contains(uriContent.Body.String(), credential.Secret) {
		t.Fatalf("uri subscription: %d %s", uriContent.Code, uriContent.Body.String())
	}
	base64Content := owner.call("GET", token.Path+"?format=base64", nil)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64Content.Body.String()))
	if base64Content.Code != 200 || err != nil || !strings.Contains(string(decoded), "vless://") {
		t.Fatalf("base64 subscription: %d %s", base64Content.Code, base64Content.Body.String())
	}
	singboxContent := owner.call("GET", token.Path+"?format=sing-box", nil)
	if singboxContent.Code != 200 || !strings.Contains(singboxContent.Body.String(), "203.0.113.10") || !strings.Contains(singboxContent.Body.String(), credential.Secret) {
		t.Fatalf("sing-box subscription: %d %s", singboxContent.Code, singboxContent.Body.String())
	}

	// Step 5b (Phase 5): the Owner drops the quota below the already-counted
	// usage. The panel evaluates the transition, builds and deploys an automatic
	// revision without the user's credential, and the running Agent applies it.
	quotaResponse := owner.call("PUT", "/api/v1/users/"+account.ID, user.Input{Username: account.Username, DisplayName: account.DisplayName, Status: account.Status, ExpiresAt: account.ExpiresAt, TrafficLimit: 300, MaxDevices: account.MaxDevices, MaxConnections: account.MaxConnections, AllowedProtocols: account.AllowedProtocols, NodeGroupIDs: account.NodeGroupIDs})
	if quotaResponse.Code != 200 {
		t.Fatalf("quota edit: %d %s", quotaResponse.Code, quotaResponse.Body.String())
	}
	var autoRevision deployment.Revision
	flowWait(t, "an automatic quota revision", func() bool {
		items, _, err := deployments.List(ctx, 1, 100)
		if err != nil {
			return false
		}
		for _, item := range items {
			if item.Author == "auto" {
				autoRevision = item
				return true
			}
		}
		return false
	})
	state, err := app.deployments.State(ctx, autoRevision.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range state.Credentials {
		if value.Secret == credential.Secret {
			t.Fatal("over-quota credential survived in the automatic revision")
		}
	}
	flowWait(t, "the fake core to apply the automatic revision without the user credential", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, configuration := range fake.applied {
			if configuration.Revision == autoRevision.ID {
				return !strings.Contains(string(configuration.Data), credential.Secret)
			}
		}
		return false
	})
	flowWait(t, "the panel to record the automatic deployment as successful", func() bool {
		status, err := deployments.TargetStatus(ctx, pending.ID, autoRevision.ID)
		return err == nil && status == "success"
	})

	// Step 8: back up, change data, and confirm the backup restores the user,
	// node group and policy. The live in-place file swap is a process-level
	// operation (cmd/panel restoreAndServe); here the archive is staged with the
	// same production prepare path and read back.
	password := "coreflow-backup-password"
	backupResponse := owner.call("POST", "/api/v1/backups", map[string]any{"password": password})
	if backupResponse.Code != 201 {
		t.Fatalf("backup: %d %s", backupResponse.Code, backupResponse.Body.String())
	}
	var backup struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(backupResponse.Body.Bytes(), &backup); err != nil || backup.ID == "" {
		t.Fatalf("backup id: %v %s", err, backupResponse.Body.String())
	}
	if verifyResponse := owner.call("POST", "/api/v1/backups/"+backup.ID+"/verify", map[string]any{"password": password}); verifyResponse.Code != 200 {
		t.Fatalf("backup verify: %d %s", verifyResponse.Code, verifyResponse.Body.String())
	}
	if deleteResponse := owner.call("DELETE", "/api/v1/users/"+account.ID, nil); deleteResponse.Code != 204 {
		t.Fatalf("delete user: %d %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if listResponse := owner.call("GET", "/api/v1/users", nil); strings.Contains(listResponse.Body.String(), account.ID) {
		t.Fatal("deleted user still present in the live panel")
	}
	flowVerifyRestoredBackup(t, ctx, app, backup.ID, password, account.ID, group.ID)
}

func flowVerifyRestoredBackup(t *testing.T, ctx context.Context, app *Server, backupID, password, userID, groupID string) {
	t.Helper()
	archivePath := filepath.Join(filepath.Dir(app.Config.Database), "backups", backupID+".pcb")
	targetDirectory := t.TempDir()
	target := app.Config
	target.Database = filepath.Join(targetDirectory, "panel.db")
	target.NodeIdentityDirectory = filepath.Join(targetDirectory, "identity")
	prepared, err := archive.PrepareRestore(ctx, t.TempDir(), archivePath, password, archive.RestoreTarget{Panel: target, ConfigurationPath: filepath.Join(targetDirectory, "panel.yaml")})
	if err != nil {
		t.Fatalf("prepare restore: %v", err)
	}
	staged := filepath.Join(prepared.Directory, "database", "panel.db")
	restored, err := db.Open(ctx, staged)
	if err != nil {
		t.Fatalf("open staged database: %v", err)
	}
	defer restored.Close()
	for table, id := range map[string]string{"users": userID, "node_groups": groupID} {
		var count int
		if err := restored.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=?", id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("restored %s %q missing: %d %v", table, id, count, err)
		}
	}
	var policies int
	if err := restored.QueryRowContext(ctx, "SELECT count(*) FROM policies WHERE name=?", "Flow block").Scan(&policies); err != nil || policies != 1 {
		t.Fatalf("restored policy missing: %d %v", policies, err)
	}
}
