package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/panel/auth"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/testutil/dbtest"
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
	restored, err := dbtest.Open(ctx, staged)
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
