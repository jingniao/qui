package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

// devicesRemovedCode is the error code every removed device route answers.
const devicesRemovedCode = "devices_removed"

// publishNodeRevision creates and deploys a proxy revision for one node and
// applies it as the Agent would, so the node has a published state to compare
// against.
func publishNodeRevision(t *testing.T, f *quotaFixture, nodeID, summary string) int64 {
	t.Helper()
	response := f.owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": nodeID, "summary": summary})
	if response.Code != 201 {
		t.Fatalf("revision for %s: %d %s", nodeID, response.Code, response.Body.String())
	}
	var revision deployment.Revision
	if err := json.Unmarshal(response.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	if deployResponse := f.owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil); deployResponse.Code != 202 {
		t.Fatalf("deploy for %s: %d %s", nodeID, deployResponse.Code, deployResponse.Body.String())
	}
	if err := f.app.deployments.Result(f.ctx, nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	f.idle(t)
	return revision.ID
}

// A5.2: after the 0113 migration a node whose published state still carries a
// credential of a device that no longer exists must be reconciled once, while a
// node that is already consistent must not receive a revision. The startup pass
// is the only thing that can notice this, because QuotaState was seeded with the
// post-migration database and therefore sees no activity transition.
func TestStartupReconciliationAfterUserLevelMigration(t *testing.T) {
	ctx := context.Background()
	f := newQuotaFixture(t)
	// The first node is published before any user is granted to its group, so its
	// published state has no user access surface at all and the startup pass has
	// nothing to correct there.
	f.publish(t)
	f.markRecovered()

	nodes := &node.Repository{Database: f.app.Database}
	second, _, _, err := nodes.CreateEnrollment(ctx, "Second Node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	groupResponse := f.owner.call("POST", "/api/v1/node-groups", node.GroupInput{Name: "Second Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{second.ID}})
	if groupResponse.Code != 201 {
		t.Fatalf("second group: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	var secondGroup node.Group
	if err := json.Unmarshal(groupResponse.Body.Bytes(), &secondGroup); err != nil {
		t.Fatal(err)
	}
	inboundResponse := f.owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "Second VLESS", NodeID: second.ID, Protocol: "vless", Listen: "0.0.0.0", Port: 24444, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if inboundResponse.Code != 201 {
		t.Fatalf("second inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	userResponse := f.owner.call("POST", "/api/v1/users", user.Input{Username: "migrated_user", DisplayName: "Migrated", Status: "active", AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{secondGroup.ID}})
	if userResponse.Code != 201 {
		t.Fatalf("user: %d %s", userResponse.Code, userResponse.Body.String())
	}
	var account user.User
	if err := json.Unmarshal(userResponse.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	identity, err := f.app.quotaDeployer.users.AccessIdentity(ctx, account.ID)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	credentials, err := f.app.quotaDeployer.users.Credentials(ctx, identity.ID, true)
	if err != nil || len(credentials) != 1 || credentials[0].Secret == "" {
		t.Fatalf("credentials: %v %v", err, credentials)
	}
	credential := credentials[0]
	publishNodeRevision(t, f, second.ID, "Second node baseline")
	f.markRecovered()

	// What migration 0113 did to the extra devices: the row and its credentials go
	// away and only the credential tombstones remain, so the node keeps running a
	// credential the panel no longer knows.
	if err := f.app.quotaDeployer.users.DeleteDevice(ctx, identity.ID); err != nil {
		t.Fatalf("delete the access identity: %v", err)
	}
	var tombstones int
	if err := f.app.Database.QueryRowContext(ctx, "SELECT count(*) FROM credential_tombstones WHERE credential_id=?", credential.ID).Scan(&tombstones); err != nil {
		t.Fatal(err)
	}
	if tombstones == 0 {
		t.Fatal("the removed credential left no tombstone")
	}

	restarted := restartQuotaDeployer(t, f)
	restarted.recover(ctx)
	flowWait(t, "the startup pass to reconcile the affected node", func() bool { return len(f.autoRevisions(t)) >= 1 })
	// Wait past the restarted deployer's coalescing window so a late revision for
	// the consistent node cannot escape the exact-count assertion below.
	time.Sleep(3 * restarted.window)

	automatic := f.autoRevisions(t)
	if len(automatic) != 1 {
		t.Fatalf("the startup pass created %d automatic revisions, want 1", len(automatic))
	}
	if automatic[0].NodeID != second.ID {
		t.Fatalf("the automatic revision targeted %q, want the affected node %q", automatic[0].NodeID, second.ID)
	}
	if stateHasCredential(f.state(t, automatic[0].ID), credential) {
		t.Fatal("the removed credential survived the startup reconciliation")
	}
}

// A5.5: a 1.0.9-shaped Agent keeps working against a 1.1 panel. The Agent wire
// contract did not change in Phase 5, so the harness below (a real control loop
// with a fake core runtime) *is* such an Agent: it publishes, rolls back and
// reports a traffic batch that the panel charges to the user's single access
// identity.
func TestPanelPublishesRollsBackAndAccountsTrafficWithALegacyAgent(t *testing.T) {
	ctx := context.Background()
	app := testServer(t)
	app.Config.ShutdownTimeout = time.Second
	address, stop := startControl(t, app)
	defer stop()
	owner := flowOwner(t, app)

	nodes := &node.Repository{Database: app.Database}
	pending, token, _, err := nodes.CreateEnrollment(ctx, "Legacy Agent Node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.SetPublicIP(ctx, pending.ID, "203.0.113.20"); err != nil {
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
	control.NewRuntime = func(string, string) core.Runtime { return fake }
	t.Cleanup(func() { control.NewRuntime = previousRuntime })
	if err := control.Enroll(ctx, agentConfig, token, "legacy-agent"); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	startAgent := func() (context.CancelFunc, chan error) {
		agentCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- control.Run(agentCtx, agentConfig, logger) }()
		return cancel, done
	}
	stopAgent := func(cancel context.CancelFunc, done chan error) {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Agent stop timeout")
		}
	}
	cancel, done := startAgent()
	stopped := false
	// A single stopper keeps the deferred cleanup from ending the Agent twice: the
	// test stops it once before the spool write and starts it again afterwards, and
	// the cleanup must end whichever run is current.
	stopNow := func() {
		if stopped {
			return
		}
		stopped = true
		stopAgent(cancel, done)
	}
	defer stopNow()

	groupResponse := owner.call("POST", "/api/v1/node-groups", node.GroupInput{Name: "Legacy entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{pending.ID}})
	if groupResponse.Code != 201 {
		t.Fatalf("node group: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	var group node.Group
	if err := json.Unmarshal(groupResponse.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	inboundResponse := owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "Legacy VLESS", NodeID: pending.ID, Protocol: "vless", Listen: "0.0.0.0", Port: port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if inboundResponse.Code != 201 {
		t.Fatalf("inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	// The baseline holds no user credential; the second revision adds the identity's
	// credential, which is what the traffic batch below belongs to. Both are
	// delivered over the control connection and applied by the Agent itself, so the
	// test waits for the node's published revision instead of reporting a result.
	publish := func(summary string) int64 {
		response := owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": pending.ID, "summary": summary})
		if response.Code != 201 {
			t.Fatalf("revision %q: %d %s", summary, response.Code, response.Body.String())
		}
		var revision deployment.Revision
		if err := json.Unmarshal(response.Body.Bytes(), &revision); err != nil {
			t.Fatal(err)
		}
		if deployResponse := owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil); deployResponse.Code != 202 {
			t.Fatalf("deploy %q: %d %s", summary, deployResponse.Code, deployResponse.Body.String())
		}
		flowWait(t, "revision "+strconv.FormatInt(revision.ID, 10)+" to reach the node", func() bool {
			published, err := app.deployments.Published(ctx, pending.ID)
			return err == nil && published.Revision == revision.ID
		})
		return revision.ID
	}
	baseline := publish("Legacy baseline")
	userResponse := owner.call("POST", "/api/v1/users", user.Input{Username: "legacy_agent_user", DisplayName: "Legacy", Status: "active", AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
	if userResponse.Code != 201 {
		t.Fatalf("user: %d %s", userResponse.Code, userResponse.Body.String())
	}
	var account user.User
	if err := json.Unmarshal(userResponse.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	identity, err := app.quotaDeployer.users.AccessIdentity(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := app.quotaDeployer.users.Credentials(ctx, identity.ID, true)
	if err != nil || len(credentials) != 1 || credentials[0].Secret == "" {
		t.Fatalf("credentials: %v %v", err, credentials)
	}
	credential := credentials[0]
	current := publish("Legacy with the identity credential")
	state, err := app.deployments.State(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	if !stateHasCredential(state, credential) {
		t.Fatal("the published revision does not carry the identity's credential")
	}

	// The Agent goes offline and queues one traffic batch for that credential; the
	// panel must charge it to the user when the Agent returns.
	stopNow()
	queue, err := spool.Open(ctx, filepath.Join(agentConfig.DataDirectory, "spool"), pending.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	counter := traffic.Counter{CoreEpoch: "legacy-agent-epoch", Revision: current, CredentialID: credential.ID, UserID: account.ID, DeviceID: identity.ID, Protocol: "vless", UploadBytes: 100, DownloadBytes: 200, ObservedAt: now}
	if err := queue.SaveTraffic(ctx, []traffic.Counter{counter}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	cancel, done = startAgent()
	stopped = false
	flowWait(t, "the Agent's traffic batch to be accounted to the user", func() bool {
		var used int64
		err := app.Database.QueryRowContext(ctx, "SELECT used_traffic FROM users WHERE id=?", account.ID).Scan(&used)
		return err == nil && used == 300
	})

	// Rolling back to the credential-free baseline removes the user's access from
	// the node again, and the Agent reports the rollback.
	rollbackResponse := owner.call("POST", "/api/v1/nodes/"+pending.ID+"/rollback", map[string]any{"revision": baseline})
	if rollbackResponse.Code != 202 {
		t.Fatalf("rollback: %d %s", rollbackResponse.Code, rollbackResponse.Body.String())
	}
	flowWait(t, "the rollback to reach the node", func() bool {
		published, err := app.deployments.Published(ctx, pending.ID)
		return err == nil && published.Revision == baseline
	})
}

// A5.6: allowing one more protocol adds its credential with the next automatic
// revision, and resetting the credentials revokes the old secrets on the node. The
// fixture's entry inbound is VLESS, so the second protocol is SOCKS5, which the
// quick user-access draft creates; the property under test is the same one the
// plan names for socks5 -> trojan.
func TestProtocolChangeAddsAndResetRevokesCredentials(t *testing.T) {
	f := newQuotaFixture(t)
	account, vlessCredential := f.addProtocolUser(t, "protocol_user", "", "vless")
	f.publish(t)
	f.quickAccess(t, "socks5", "0.0.0.0", 1080)
	f.markRecovered()
	f.settle()

	// Allowing SOCKS5 as well must issue its credential and publish it.
	account.AllowedProtocols = []string{"vless", "socks5"}
	saveUser(t, f, account, 0)
	flowWait(t, "the automatic revision with the new protocol credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	added := f.autoRevisions(t)[0]
	addedState := f.state(t, added.ID)
	if !stateHasCredential(addedState, vlessCredential) {
		t.Fatal("the automatic revision dropped the existing VLESS credential")
	}
	identity, err := f.app.quotaDeployer.users.AccessIdentity(f.ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	protocolCredentials, err := f.app.quotaDeployer.users.Credentials(f.ctx, identity.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	socks5Credential := user.Credential{}
	for _, credential := range protocolCredentials {
		if credential.Protocol == "socks5" && credential.Status == "active" {
			socks5Credential = credential
		}
	}
	if socks5Credential.ID == "" || socks5Credential.Secret == "" {
		t.Fatalf("no SOCKS5 credential was issued: %+v", protocolCredentials)
	}
	if !stateHasCredential(addedState, socks5Credential) {
		t.Fatal("the automatic revision does not carry the new SOCKS5 credential")
	}
	f.apply(t, added.ID)
	f.idle(t)

	// A reset re-issues every credential; the next automatic revision must carry the
	// new secrets and none of the old ones.
	if response := f.owner.call("POST", "/api/v1/users/"+account.ID+"/credentials/reset", nil); response.Code != 204 {
		t.Fatalf("reset credentials: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "the automatic revision after the reset", func() bool {
		return len(f.autoRevisions(t)) == 2
	})
	reset := f.autoRevisions(t)[1]
	resetState := f.state(t, reset.ID)
	if stateHasCredential(resetState, vlessCredential) || stateHasCredential(resetState, socks5Credential) {
		t.Fatal("the old secrets survived the credential reset")
	}
	renewed, err := f.app.quotaDeployer.users.Credentials(f.ctx, identity.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	fresh := 0
	for _, credential := range renewed {
		if credential.Status == "active" && credential.Secret != "" && stateHasCredential(resetState, credential) {
			fresh++
		}
	}
	if fresh != 2 {
		t.Fatalf("the reset revision carries %d of the two renewed credentials: %+v", fresh, resetState.Credentials)
	}
}

// Every route of the removed device API answers 410 devices_removed instead of
// 404, so a stale caller learns why its request no longer works, and none of them
// can read or write anything. The list mirrors the 410 registration in
// internal/panel/user/handler_http.go; the subscription token endpoint refuses the
// removed deviceId field the same way.
func TestRemovedDeviceRoutesAnswer410(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	paths := []struct{ method, path string }{
		{"GET", "/api/v1/users/dummy/devices"},
		{"POST", "/api/v1/users/dummy/devices"},
		{"GET", "/api/v1/devices"},
		{"GET", "/api/v1/devices/dummy"},
		{"PUT", "/api/v1/devices/dummy"},
		{"DELETE", "/api/v1/devices/dummy"},
		{"POST", "/api/v1/devices/dummy/credentials/socks5/reset"},
		{"DELETE", "/api/v1/devices/dummy/credentials/socks5"},
	}
	for _, item := range paths {
		response := owner.call(item.method, item.path, map[string]any{})
		if response.Code != 410 || !strings.Contains(response.Body.String(), devicesRemovedCode) {
			t.Fatalf("%s %s answered %d %s", item.method, item.path, response.Code, response.Body.String())
		}
	}
	response := owner.call("POST", "/api/v1/users/dummy/subscription-token", map[string]any{"deviceId": "x"})
	if response.Code != 410 || !strings.Contains(response.Body.String(), devicesRemovedCode) {
		t.Fatalf("subscription-token with deviceId answered %d %s", response.Code, response.Body.String())
	}
}
