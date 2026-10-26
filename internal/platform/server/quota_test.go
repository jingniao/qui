package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// quotaFixture is the smallest end-to-end panel state that exercises the
// automatic quota redeploy: an enrolled node with a published revision, a
// subscription node group, a VLESS inbound, and users with credentials.
type quotaFixture struct {
	app     *Server
	owner   flowHTTP
	ctx     context.Context
	nodeID  string
	groupID string
}

func newQuotaFixture(t *testing.T) *quotaFixture {
	t.Helper()
	ctx := context.Background()
	app := testServer(t)
	app.quotaDeployer.window = 120 * time.Millisecond
	owner := flowOwner(t, app)
	nodes := &node.Repository{Database: app.Database}
	pending, _, _, err := nodes.CreateEnrollment(ctx, "Quota Node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	groupResponse := owner.call("POST", "/api/v1/node-groups", node.GroupInput{Name: "Quota Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{pending.ID}})
	if groupResponse.Code != 201 {
		t.Fatalf("node group: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	var group node.Group
	if err := json.Unmarshal(groupResponse.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	inboundResponse := owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "Quota VLESS", NodeID: pending.ID, Protocol: "vless", Listen: "0.0.0.0", Port: 24443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if inboundResponse.Code != 201 {
		t.Fatalf("inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	return &quotaFixture{app: app, owner: owner, ctx: ctx, nodeID: pending.ID, groupID: group.ID}
}

func (f *quotaFixture) addUser(t *testing.T, username, expiresAt string) (user.User, user.Credential) {
	return f.addProtocolUser(t, username, expiresAt, "vless")
}

// addProtocolUser creates a user whose only credential is for one protocol, so a
// test can drive the quick user-access feature, which only supports HTTP and
// SOCKS5.
func (f *quotaFixture) addProtocolUser(t *testing.T, username, expiresAt, protocol string) (user.User, user.Credential) {
	t.Helper()
	response := f.owner.call("POST", "/api/v1/users", user.Input{Username: username, DisplayName: username, Status: "active", ExpiresAt: expiresAt, MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{protocol}, NodeGroupIDs: []string{f.groupID}})
	if response.Code != 201 {
		t.Fatalf("user %s: %d %s", username, response.Code, response.Body.String())
	}
	var account user.User
	if err := json.Unmarshal(response.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	deviceResponse := f.owner.call("POST", "/api/v1/users/"+account.ID+"/devices", map[string]any{"name": username + " device", "protocols": []string{protocol}})
	if deviceResponse.Code != 201 {
		t.Fatalf("device %s: %d %s", username, deviceResponse.Code, deviceResponse.Body.String())
	}
	var device user.Device
	if err := json.Unmarshal(deviceResponse.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	if len(device.Credentials) != 1 || device.Credentials[0].Secret == "" || device.Credentials[0].Protocol != protocol {
		t.Fatalf("user %s has no %s credential", username, protocol)
	}
	return account, device.Credentials[0]
}

// publish builds and applies an initial revision containing every current
// credential, so the node has a published state to diff against.
func (f *quotaFixture) publish(t *testing.T) int64 {
	t.Helper()
	response := f.owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": f.nodeID, "summary": "Quota baseline"})
	if response.Code != 201 {
		t.Fatalf("revision: %d %s", response.Code, response.Body.String())
	}
	var revision deployment.Revision
	if err := json.Unmarshal(response.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	if deployResponse := f.owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil); deployResponse.Code != 202 {
		t.Fatalf("deploy: %d %s", deployResponse.Code, deployResponse.Body.String())
	}
	if err := f.app.deployments.Result(f.ctx, f.nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	f.idle(t)
	return revision.ID
}

// idle waits until the reconcile the last successful deploy queued has run.
// Every success queues one; a setup step that returned before it ran would let
// it fire in the middle of the test's next step, which on a slow runner (the
// race detector) either holds the node with an automatic revision or reacts to
// a change the test has only just made.
func (f *quotaFixture) idle(t *testing.T) {
	t.Helper()
	deployer := f.app.quotaDeployer
	flowWait(t, "the post-deploy reconcile", func() bool {
		deployer.mu.Lock()
		defer deployer.mu.Unlock()
		return deployer.pending[f.nodeID] == nil && deployer.timers[f.nodeID] == nil && deployer.flushing == 0
	})
}

// apply simulates the agent reporting a successful apply, advancing the
// node's published runtime state. A real node applies its automatic revision
// through its agent; the test must do the same or the next flush would treat
// the node as unchanged and skip the deploy.
func (f *quotaFixture) apply(t *testing.T, revision int64) {
	t.Helper()
	if err := f.app.deployments.Result(f.ctx, f.nodeID, &pb.ApplyResult{Revision: revision, Success: true}); err != nil {
		t.Fatal(err)
	}
}

func (f *quotaFixture) autoRevisions(t *testing.T) []deployment.Revision {
	t.Helper()
	revisions, _, err := f.app.deployments.List(f.ctx, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	automatic := []deployment.Revision{}
	for _, revision := range revisions {
		if revision.Author == "auto" {
			automatic = append(automatic, revision)
		}
	}
	sort.Slice(automatic, func(i, j int) bool { return automatic[i].ID < automatic[j].ID })
	return automatic
}

func (f *quotaFixture) state(t *testing.T, revision int64) desiredstate.State {
	t.Helper()
	state, err := f.app.deployments.State(f.ctx, revision)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func stateHasCredential(state desiredstate.State, credential user.Credential) bool {
	for _, value := range state.Credentials {
		if value.ID == credential.ID || value.Secret != "" && value.Secret == credential.Secret {
			return true
		}
	}
	return false
}

func setQuota(t *testing.T, ctx context.Context, f *quotaFixture, userID string, limit, used int64) {
	t.Helper()
	if _, err := f.app.Database.ExecContext(ctx, "UPDATE users SET traffic_limit=?,used_traffic=? WHERE id=?", limit, used, userID); err != nil {
		t.Fatal(err)
	}
}

func saveUser(t *testing.T, f *quotaFixture, account user.User, limit int64) {
	t.Helper()
	response := f.owner.call("PUT", "/api/v1/users/"+account.ID, user.Input{Username: account.Username, DisplayName: account.DisplayName, Status: account.Status, ExpiresAt: account.ExpiresAt, TrafficLimit: limit, MaxDevices: account.MaxDevices, MaxConnections: account.MaxConnections, AllowedProtocols: account.AllowedProtocols, NodeGroupIDs: account.NodeGroupIDs})
	if response.Code != 200 {
		t.Fatalf("save user: %d %s", response.Code, response.Body.String())
	}
}

type alertRecorder struct {
	mu     sync.Mutex
	alerts []map[string]string
}

func (r *alertRecorder) record(payload map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, payload)
}

func (r *alertRecorder) reasons() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []string{}
	for _, alert := range r.alerts {
		result = append(result, alert["reason"])
	}
	return result
}

// payloads returns the recorded alerts of one kind, for example
// user_access_failed.
func (r *alertRecorder) payloads(kind string) []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []map[string]string{}
	for _, alert := range r.alerts {
		if alert["alert"] == kind {
			result = append(result, alert)
		}
	}
	return result
}

func (f *quotaFixture) recordAlerts() *alertRecorder {
	recorder := &alertRecorder{}
	f.app.quotaDeployer.publish = func(kind string, data any) {
		if kind != "system.alert" {
			return
		}
		if payload, ok := data.(map[string]string); ok {
			recorder.record(payload)
		}
	}
	return recorder
}

func TestQuotaCrossingDeploysOnceWithoutCredential(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "cross_user", "")
	f.publish(t)
	alerts := f.recordAlerts()

	setQuota(t, f.ctx, f, account.ID, 50, 100)
	saveUser(t, f, account, 50)
	flowWait(t, "one automatic revision removing the credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("over-quota credential survived in the automatic revision")
	}
	if automatic.NodeID != f.nodeID {
		t.Fatalf("automatic revision targeted %q", automatic.NodeID)
	}
	// Repeated reports while still over quota must not create more revisions,
	// and a steady state must not deploy at all.
	for index := 0; index < 3; index++ {
		f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	}
	f.app.quotaDeployer.tickOnce(f.ctx)
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("repeated reports produced %d automatic revisions", count)
	}
	if reasons := alerts.reasons(); len(reasons) != 1 || reasons[0] != "quota_exceeded" {
		t.Fatalf("alerts: %v", reasons)
	}
}

func TestQuotaResetReactivatesAndRestoresCredential(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "reset_user", "")
	f.publish(t)
	setQuota(t, f.ctx, f, account.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	flowWait(t, "the over-quota removal", func() bool { return len(f.autoRevisions(t)) == 1 })
	f.apply(t, f.autoRevisions(t)[0].ID)
	alerts := f.recordAlerts()

	if response := f.owner.call("POST", "/api/v1/users/"+account.ID+"/reset-traffic", nil); response.Code != 204 {
		t.Fatalf("reset traffic: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "one reactivation revision restoring the credential", func() bool { return len(f.autoRevisions(t)) == 2 })
	reactivated := f.autoRevisions(t)[1]
	if !stateHasCredential(f.state(t, reactivated.ID), credential) {
		t.Fatal("reactivated credential missing from the automatic revision")
	}
	if reasons := alerts.reasons(); len(reasons) != 1 || reasons[0] != "reactivated" {
		t.Fatalf("alerts: %v", reasons)
	}
	// Staying active produces no further deploys.
	f.app.quotaDeployer.tickOnce(f.ctx)
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 2 {
		t.Fatalf("steady state produced %d automatic revisions", count)
	}
}

func TestExpiredUserRemovedByTickWithoutTraffic(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "expiry_user", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	f.publish(t)
	if _, err := f.app.Database.ExecContext(f.ctx, "UPDATE users SET expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05.000000000Z"), account.ID); err != nil {
		t.Fatal(err)
	}
	f.app.quotaDeployer.tickOnce(f.ctx)
	flowWait(t, "the tick to remove the expired credential", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("expired credential survived in the automatic revision")
	}
	for index := 0; index < 2; index++ {
		f.app.quotaDeployer.tickOnce(f.ctx)
	}
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("repeated ticks produced %d automatic revisions", count)
	}
}

func TestTwoCrossingsCoalesceIntoOneDeploy(t *testing.T) {
	f := newQuotaFixture(t)
	first, firstCredential := f.addUser(t, "coalesce_a", "")
	second, secondCredential := f.addUser(t, "coalesce_b", "")
	f.publish(t)
	setQuota(t, f.ctx, f, first.ID, 50, 100)
	setQuota(t, f.ctx, f, second.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{first.ID, second.ID})
	flowWait(t, "one coalesced automatic revision", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	state := f.state(t, automatic.ID)
	if stateHasCredential(state, firstCredential) || stateHasCredential(state, secondCredential) {
		t.Fatal("coalesced revision kept an over-quota credential")
	}
	for _, username := range []string{"coalesce_a", "coalesce_b"} {
		if !strings.Contains(automatic.Summary, "user "+username+" ") {
			t.Fatalf("summary %q does not mention %s", automatic.Summary, username)
		}
	}
}

func TestQuotaStatesEqualIgnoresRevision(t *testing.T) {
	base := desiredstate.State{SchemaVersion: 1, Revision: 3, NodeID: "n", Users: []desiredstate.User{}, Credentials: []desiredstate.Credential{{ID: "c1"}}}
	same := base
	same.Revision = 9
	if !quotaStatesEqual(base, same) {
		t.Fatal("revision label should not affect equality")
	}
	different := base
	different.Credentials = []desiredstate.Credential{{ID: "c2"}}
	if quotaStatesEqual(base, different) {
		t.Fatal("different credentials must not compare equal")
	}
}

func stateUser(t *testing.T, state desiredstate.State, userID string) desiredstate.User {
	t.Helper()
	for _, entry := range state.Users {
		if entry.ID == userID {
			return entry
		}
	}
	t.Fatalf("user %s missing from the state", userID)
	return desiredstate.User{}
}

// renewUser drives the Owner HTTP Save path with a new expiry and returns the
// stored expiry, exactly as the fresh render would carry it.
func renewUser(t *testing.T, f *quotaFixture, account user.User, expiresAt string) string {
	t.Helper()
	response := f.owner.call("PUT", "/api/v1/users/"+account.ID, user.Input{Username: account.Username, DisplayName: account.DisplayName, Status: account.Status, ExpiresAt: expiresAt, TrafficLimit: account.TrafficLimit, MaxDevices: account.MaxDevices, MaxConnections: account.MaxConnections, AllowedProtocols: account.AllowedProtocols, NodeGroupIDs: account.NodeGroupIDs})
	if response.Code != 200 {
		t.Fatalf("renew user: %d %s", response.Code, response.Body.String())
	}
	var saved user.User
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	return saved.ExpiresAt
}

// TestQuotaRenewalOfActiveUserRedeploysNewExpiry is the core fix: the agent
// enforces ExpiresAt itself, so extending an active user's deadline must reach
// the nodes. The renewal is not an activity transition, so it redeploys without
// a system.alert, and repeated ticks must not produce further revisions.
func TestQuotaRenewalOfActiveUserRedeploysNewExpiry(t *testing.T) {
	f := newQuotaFixture(t)
	account, _ := f.addUser(t, "renewed_user", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	baseline := f.publish(t)
	alerts := f.recordAlerts()

	published := stateUser(t, f.state(t, baseline), account.ID)
	renewed := renewUser(t, f, account, time.Now().UTC().Add(72*time.Hour).Format(time.RFC3339))
	if renewed == published.ExpiresAt {
		t.Fatalf("test did not actually change the expiry (%s)", renewed)
	}
	flowWait(t, "one automatic revision carrying the new expiry", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	if got := stateUser(t, f.state(t, automatic.ID), account.ID).ExpiresAt; got != renewed {
		t.Fatalf("automatic revision kept %q, want the renewed %q", got, renewed)
	}
	if got := stateUser(t, f.state(t, automatic.ID), account.ID).ExpiresAt; got == published.ExpiresAt {
		t.Fatal("automatic revision still carries the old expiry")
	}
	if reasons := alerts.reasons(); len(reasons) != 0 {
		t.Fatalf("a renewal must not publish alerts: %v", reasons)
	}
	// The one-minute tick must not refresh: the user is active and unchanged.
	f.apply(t, automatic.ID)
	for index := 0; index < 2; index++ {
		f.app.quotaDeployer.tickOnce(f.ctx)
	}
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("ticks produced %d automatic revisions", count)
	}
}

// TestQuotaSaveWithoutAccessChangeDoesNotDeploy proves the renewal hook is not
// a general "save deploys" switch: an Owner Save that changes nothing leaves
// the node on its published revision.
func TestQuotaSaveWithoutAccessChangeDoesNotDeploy(t *testing.T) {
	f := newQuotaFixture(t)
	account, _ := f.addUser(t, "steady_user", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	f.publish(t)
	alerts := f.recordAlerts()

	saveUser(t, f, account, account.TrafficLimit)
	f.app.quotaDeployer.tickOnce(f.ctx)
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("an unchanged Save produced %d automatic revisions", count)
	}
	if reasons := alerts.reasons(); len(reasons) != 0 {
		t.Fatalf("an unchanged Save published alerts: %v", reasons)
	}
}

// hideTable renames a table away so the next read of it fails, and returns the
// function that puts it back. It exercises the paths where a read error has to
// delay the work instead of dropping it.
func hideTable(t *testing.T, ctx context.Context, f *quotaFixture, table string) func() {
	t.Helper()
	if _, err := f.app.Database.ExecContext(ctx, "ALTER TABLE "+table+" RENAME TO "+table+"_hidden"); err != nil {
		t.Fatalf("hide %s: %v", table, err)
	}
	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		if _, err := f.app.Database.ExecContext(ctx, "ALTER TABLE "+table+"_hidden RENAME TO "+table); err != nil {
			t.Fatalf("restore %s: %v", table, err)
		}
	}
}

func countAdminOperations(t *testing.T, f *quotaFixture, action string) int {
	t.Helper()
	var count int
	if err := f.app.Database.QueryRowContext(f.ctx, "SELECT count(*) FROM admin_operations WHERE action=?", action).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// restartQuotaDeployer builds the deployer a restarted panel would have: a
// fresh QuotaState seeded from the current database, so the pre-restart state is
// the baseline and no transition exists for it. The old deployer is stopped as
// well, because a restart ends it: a deploy it saw before the restart would
// otherwise queue a reconciliation that races the one the test drives.
func restartQuotaDeployer(t *testing.T, f *quotaFixture) *quotaDeployer {
	t.Helper()
	f.stopDeployer(f.app.quotaDeployer)
	state := user.NewQuotaState(f.app.quotaDeployer.users)
	if err := state.Seed(f.ctx); err != nil {
		t.Fatal(err)
	}
	restarted := newQuotaDeployer(f.app.quotaDeployer.logger, f.app.quotaDeployer.users, state, f.app.quotaDeployer.proxy, f.app.quotaDeployer.nodes, f.app.quotaDeployer.deployments, f.app.quotaDeployer.publish)
	restarted.window = f.app.quotaDeployer.window
	restarted.record = f.app.quotaDeployer.record
	return restarted
}

// stopDeployer drops everything a deployer still has queued.
func (f *quotaFixture) stopDeployer(d *quotaDeployer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for nodeID, timer := range d.timers {
		if timer != nil {
			timer.Stop()
		}
		d.timers[nodeID] = nil
	}
	d.pending = map[string]*quotaPending{}
}

// quotaFlakySnapshotter fails the Owner render until it is switched back on, so
// the published-only fallback can be exercised.
type quotaFlakySnapshotter struct {
	mu    sync.Mutex
	inner quotaSnapshotter
	fail  bool
}

func (s *quotaFlakySnapshotter) Snapshot(ctx context.Context, nodeID string, users proxy.AccessReader) (desiredstate.State, error) {
	s.mu.Lock()
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return desiredstate.State{}, errors.New("the saved draft cannot be rendered")
	}
	return s.inner.Snapshot(ctx, nodeID, users)
}

func (s *quotaFlakySnapshotter) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

// TestQuotaDeployFailureKeepsTheEntryForTheNextWindow is problem (a): a failed
// deploy used to drop the work, leaving the over-quota user authorized on the
// node with no transition left to fix it. The draft survives and is queued by
// the next window instead of being created twice.
func TestQuotaDeployFailureKeepsTheEntryForTheNextWindow(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "deploy_retry_user", "")
	f.publish(t)
	deployer := f.app.quotaDeployer
	deployer.window = time.Hour // this test drives the windows by hand
	setQuota(t, f.ctx, f, account.ID, 50, 100)

	restore := hideTable(t, f.ctx, f, "deployment_targets")
	deployer.enqueue(f.nodeID, user.Transition{UserID: account.ID, Username: account.Username, Reason: "quota_exceeded"})
	deployer.flush(f.nodeID)
	restore()
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("a failed deploy created %d revisions", count)
	}

	deployer.flush(f.nodeID)
	automatic := f.autoRevisions(t)
	if len(automatic) != 1 {
		t.Fatalf("the retry created a duplicate revision: %d", len(automatic))
	}
	if stateHasCredential(f.state(t, automatic[0].ID), credential) {
		t.Fatal("over-quota credential survived the retry")
	}
	status, err := f.app.deployments.TargetStatus(f.ctx, f.nodeID, automatic[0].ID)
	if err != nil || status != "pending" {
		t.Fatalf("the retried revision was not queued: status=%q error=%v", status, err)
	}
}

// TestQuotaScheduleReadFailureIsRetriedOnTick is problem (d): a transition whose
// node set could not be read used to be dropped before it was ever enqueued.
func TestQuotaScheduleReadFailureIsRetriedOnTick(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "schedule_retry_user", "")
	f.publish(t)
	alerts := f.recordAlerts()
	setQuota(t, f.ctx, f, account.ID, 50, 100)

	restore := hideTable(t, f.ctx, f, "user_node_group_grants")
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	time.Sleep(2 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("a failed node read produced %d revisions", count)
	}

	restore()
	f.app.quotaDeployer.tickOnce(f.ctx)
	flowWait(t, "the held transition to deploy once the read works again", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("over-quota credential survived the retry")
	}
	if published := alerts.payloads("user_access"); len(published) != 1 || published[0]["reason"] != "quota_exceeded" {
		t.Fatalf("transition alerts: %v", published)
	}
}

// TestQuotaAgentFailureRetriesWithANewRevisionAndGivesUp is problem (c): the
// node reports a rollback, so the automatic revision did not take effect.
// Re-deploying it would be a no-op, so a new revision is created; after
// quotaAttemptLimit failures the retries stop and the operator is told.
func TestQuotaAgentFailureRetriesWithANewRevisionAndGivesUp(t *testing.T) {
	f := newQuotaFixture(t)
	account, _ := f.addUser(t, "agent_failure_user", "")
	f.publish(t)
	alerts := f.recordAlerts()
	setQuota(t, f.ctx, f, account.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})

	for attempt := 0; attempt < quotaAttemptLimit; attempt++ {
		flowWait(t, "the next automatic revision", func() bool { return len(f.autoRevisions(t)) == attempt+1 })
		revision := f.autoRevisions(t)[attempt]
		if err := f.app.deployments.Result(f.ctx, f.nodeID, &pb.ApplyResult{Revision: revision.ID, RolledBack: true}); err != nil {
			t.Fatalf("rollback result for revision %d: %v", revision.ID, err)
		}
	}
	flowWait(t, "the give-up alert", func() bool { return len(alerts.payloads("user_access_failed")) == 1 })
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != quotaAttemptLimit {
		t.Fatalf("%d revisions after the retries stopped", count)
	}
	if count := countAdminOperations(t, f, "自动额度发布失败"); count != 1 {
		t.Fatalf("admin operations recording the failure: %d", count)
	}
	if published := alerts.payloads("user_access_failed"); published[0]["nodeId"] != f.nodeID {
		t.Fatalf("failure alert named the wrong node: %v", published[0])
	}
}

// TestQuotaStaleDraftDoesNotRevertOwnerRevision is problem (b): the node was
// busy, so the automatic revision stayed a draft. The Owner revision is
// published in the meantime, and the retry must not queue the stale draft
// (which would revert the Owner's change) but a new revision built from what
// the node now runs.
func TestQuotaStaleDraftDoesNotRevertOwnerRevision(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "owner_race_user", "")
	f.publish(t)
	alerts := f.recordAlerts()
	deployer := f.app.quotaDeployer
	deployer.window = time.Hour // this test drives the windows by hand

	outboundResponse := f.owner.call("POST", "/api/v1/outbounds", proxy.OutboundInput{Name: "Owner exit", NodeID: f.nodeID, Protocol: "direct", Enabled: true, FailurePolicy: "Block"})
	if outboundResponse.Code != 201 {
		t.Fatalf("owner outbound: %d %s", outboundResponse.Code, outboundResponse.Body.String())
	}
	var outbound proxy.Outbound
	if err := json.Unmarshal(outboundResponse.Body.Bytes(), &outbound); err != nil {
		t.Fatal(err)
	}
	revisionResponse := f.owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": f.nodeID, "summary": "Owner revision"})
	if revisionResponse.Code != 201 {
		t.Fatalf("owner revision: %d %s", revisionResponse.Code, revisionResponse.Body.String())
	}
	var ownerRevision deployment.Revision
	if err := json.Unmarshal(revisionResponse.Body.Bytes(), &ownerRevision); err != nil {
		t.Fatal(err)
	}
	if deployResponse := f.owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(ownerRevision.ID, 10)+"/deploy", nil); deployResponse.Code != 202 {
		t.Fatalf("owner deploy: %d %s", deployResponse.Code, deployResponse.Body.String())
	}

	setQuota(t, f.ctx, f, account.ID, 50, 100)
	deployer.evaluate(f.ctx, []string{account.ID})
	deployer.flush(f.nodeID)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("the busy node produced %d automatic revisions", count)
	}
	draft := f.autoRevisions(t)[0]
	if status, err := f.app.deployments.TargetStatus(f.ctx, f.nodeID, draft.ID); err != nil || status != "draft" {
		t.Fatalf("a busy node must leave the revision a draft: status=%q error=%v", status, err)
	}
	// Waiting for the busy node neither counts an attempt nor creates a second
	// draft: the same draft is reused.
	for index := 0; index < quotaAttemptLimit+1; index++ {
		deployer.flush(f.nodeID)
	}
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("the busy windows created %d automatic revisions", count)
	}
	if count := countAdminOperations(t, f, "自动额度发布失败"); count != 0 {
		t.Fatalf("waiting for the busy node was reported as a failure: %d", count)
	}
	if status, err := f.app.deployments.TargetStatus(f.ctx, f.nodeID, draft.ID); err != nil || status != "draft" {
		t.Fatalf("the draft must stay unqueued while the node is busy: status=%q error=%v", status, err)
	}

	// The Owner revision is applied; the retry must not queue the draft that
	// was built from the older published state.
	f.apply(t, ownerRevision.ID)
	deployer.flush(f.nodeID)
	automatic := f.autoRevisions(t)
	if len(automatic) != 2 {
		t.Fatalf("the retry produced %d automatic revisions", len(automatic))
	}
	latest := automatic[1]
	if latest.ID == draft.ID {
		t.Fatal("the stale draft was deployed after the Owner revision was published")
	}
	state := f.state(t, latest.ID)
	if stateHasCredential(state, credential) {
		t.Fatal("over-quota credential survived in the new revision")
	}
	found := false
	for _, published := range state.Outbounds {
		if published.ID == outbound.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the retry reverted the Owner's published outbound")
	}
	if status, err := f.app.deployments.TargetStatus(f.ctx, f.nodeID, draft.ID); err != nil || status != "draft" {
		t.Fatalf("the stale draft must stay unqueued: status=%q error=%v", status, err)
	}
	if reasons := alerts.reasons(); len(reasons) != 1 || reasons[0] != "quota_exceeded" {
		t.Fatalf("alerts: %v", reasons)
	}
}

// TestQuotaTransitionWhileRevisionPendingIsApplied proves a transition queued
// while an automatic revision is still on its way is not lost: the successful
// result makes the entry recompute and deploy what is still missing.
func TestQuotaTransitionWhileRevisionPendingIsApplied(t *testing.T) {
	f := newQuotaFixture(t)
	first, _ := f.addUser(t, "pending_a", "")
	second, secondCredential := f.addUser(t, "pending_b", "")
	f.publish(t)

	setQuota(t, f.ctx, f, first.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{first.ID})
	flowWait(t, "the first automatic revision", func() bool { return len(f.autoRevisions(t)) == 1 })
	pending := f.autoRevisions(t)[0]

	setQuota(t, f.ctx, f, second.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{second.ID})
	f.apply(t, pending.ID)

	flowWait(t, "the revision that removes the second credential too", func() bool {
		return len(f.autoRevisions(t)) == 2
	})
	latest := f.autoRevisions(t)[1]
	if stateHasCredential(f.state(t, latest.ID), secondCredential) {
		t.Fatal("the transition queued while the revision was pending was lost")
	}
}

// TestQuotaStartupPassRemovesUserWhoWentOverWhileDown is problem (e): the user
// crossed the limit while the panel was down, so no transition exists. The
// startup pass still removes the credential from the node that runs it.
func TestQuotaStartupPassRemovesUserWhoWentOverWhileDown(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "startup_user", "")
	f.publish(t)
	setQuota(t, f.ctx, f, account.ID, 50, 100)

	restarted := restartQuotaDeployer(t, f)
	restarted.recover(f.ctx)
	flowWait(t, "the startup pass to remove the credential", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("over-quota credential survived the startup pass")
	}
	if !strings.Contains(automatic.Summary, "startup reconcile") {
		t.Fatalf("startup summary: %q", automatic.Summary)
	}
}

// TestQuotaStartupPassLeavesInSyncNodeAlone proves the startup pass is not a
// deploy storm: a node whose published state already matches its users gets no
// revision.
func TestQuotaStartupPassLeavesInSyncNodeAlone(t *testing.T) {
	f := newQuotaFixture(t)
	f.addUser(t, "startup_idle_user", "")
	f.publish(t)

	restarted := restartQuotaDeployer(t, f)
	restarted.recover(f.ctx)
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("an in-sync node received %d automatic revisions", count)
	}
}

// TestQuotaInvalidDraftFallsBackToPublishedUsers is problem (f): an unrelated
// invalid draft makes every render fail, which used to skip the automatic
// revision entirely and leave the over-quota user authorized. The fallback
// builds the revision from the published state's own user list, and the regular
// render takes over again once it works.
func TestQuotaInvalidDraftFallsBackToPublishedUsers(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "invalid_draft_user", "")
	f.publish(t)
	alerts := f.recordAlerts()

	flaky := &quotaFlakySnapshotter{inner: f.app.quotaDeployer.proxy, fail: true}
	f.app.quotaDeployer.proxy = flaky
	setQuota(t, f.ctx, f, account.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	flowWait(t, "the published-based revision", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("over-quota credential survived the published-only revision")
	}
	f.apply(t, automatic.ID)

	flaky.setFail(false)
	if response := f.owner.call("POST", "/api/v1/users/"+account.ID+"/reset-traffic", nil); response.Code != 204 {
		t.Fatalf("reset traffic: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "the reactivated credential to come back", func() bool { return len(f.autoRevisions(t)) == 2 })
	restored := f.autoRevisions(t)[1]
	if !stateHasCredential(f.state(t, restored.ID), credential) {
		t.Fatal("the reactivated credential was not restored once the render worked again")
	}
	if reasons := alerts.reasons(); len(reasons) != 2 || reasons[0] != "quota_exceeded" || reasons[1] != "reactivated" {
		t.Fatalf("alerts: %v", reasons)
	}
}

// TestQuotaDisableAfterRemovingGrantReachesNode is problem (g): the Owner removed
// the user's node group grant and saved it without publishing, so the node keeps
// running the user's published credential while the user is in no granted group.
// A later disable must still reach that node.
func TestQuotaDisableAfterRemovingGrantReachesNode(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "removed_grant_user", "")
	f.publish(t)
	deployer := f.app.quotaDeployer
	deployer.window = time.Hour // this test drives the windows by hand
	// The startup pass is already done, so the disable below is the only thing
	// that can queue the node.
	deployer.recover(f.ctx)
	deployer.flush(f.nodeID)
	deployer.evaluate(f.ctx, []string{account.ID})

	account.NodeGroupIDs = []string{}
	saveUser(t, f, account, 0)
	published, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !stateHasCredential(published, credential) {
		t.Fatal("the node must still run the credential the grant removal did not publish")
	}

	account.Status = "disabled"
	saveUser(t, f, account, 0)
	deployer.tickOnce(f.ctx)
	deployer.flush(f.nodeID)

	automatic := f.autoRevisions(t)
	if len(automatic) != 1 {
		t.Fatalf("the disable produced %d automatic revisions", len(automatic))
	}
	if stateHasCredential(f.state(t, automatic[0].ID), credential) {
		t.Fatal("the disabled credential survived on the node that still ran it")
	}
}

// TestQuotaStartupPassRetriesAfterReadFailure is problem (h): the startup pass
// used to run once, so a node whose published state could not be read at that
// moment was skipped for the lifetime of the process. The unfinished pass is run
// again by the next tick, and the over-quota credential still goes away.
func TestQuotaStartupPassRetriesAfterReadFailure(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "startup_retry_user", "")
	f.publish(t)
	setQuota(t, f.ctx, f, account.ID, 50, 100)

	deployer := restartQuotaDeployer(t, f)
	deployer.window = time.Hour // this test drives the windows by hand
	restore := hideTable(t, f.ctx, f, "config_revisions")
	deployer.recover(f.ctx)
	restore()
	for index := 0; index < 3; index++ {
		deployer.tickOnce(f.ctx)
		deployer.flush(f.nodeID)
	}

	automatic := f.autoRevisions(t)
	if len(automatic) != 1 {
		t.Fatalf("the retried startup pass produced %d automatic revisions", len(automatic))
	}
	if stateHasCredential(f.state(t, automatic[0].ID), credential) {
		t.Fatal("over-quota credential survived the retried startup pass")
	}
}

// quickAccess publishes the quick user-access draft through the real HTTP
// handler and applies it, so the node runs the inbound that feature created.
func (f *quotaFixture) quickAccess(t *testing.T, protocol, listen string, port int) deployment.Revision {
	t.Helper()
	response := f.owner.call("POST", "/api/v1/revisions/user-access", deployment.UserAccessInput{NodeID: f.nodeID, Summary: "Quick user access", Protocol: protocol, Listen: listen, Port: port})
	if response.Code != 201 {
		t.Fatalf("user access draft: %d %s", response.Code, response.Body.String())
	}
	var revision deployment.Revision
	if err := json.Unmarshal(response.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	if deploy := f.owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(revision.ID, 10)+"/deploy", nil); deploy.Code != 202 {
		t.Fatalf("user access deploy: %d %s", deploy.Code, deploy.Body.String())
	}
	f.apply(t, revision.ID)
	f.idle(t)
	return revision
}

// markRecovered models a long-running panel, whose startup pass is over. Without
// this the next tick would reconcile every node and the test could not tell a
// correction caused by the deploy from one caused by the startup pass.
func (f *quotaFixture) markRecovered() {
	f.app.quotaDeployer.mu.Lock()
	f.app.quotaDeployer.recovered = true
	f.app.quotaDeployer.mu.Unlock()
}

// settle waits past the coalescing window, so no pending reconcile of the caller
// is still running when the test acts.
func (f *quotaFixture) settle() { time.Sleep(3 * f.app.quotaDeployer.window) }

func (f *quotaFixture) publishedInbound(t *testing.T, protocol string) desiredstate.Inbound {
	t.Helper()
	published, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return stateInbound(t, published, protocol)
}

// stateInbound returns one protocol's rendered inbound, so a test can check that
// an automatic revision kept the inbound's identity, its enablement and its
// credential bindings.
func stateInbound(t *testing.T, state desiredstate.State, protocol string) desiredstate.Inbound {
	t.Helper()
	for _, inbound := range state.Inbounds {
		if inbound.Protocol == protocol {
			return inbound
		}
	}
	t.Fatalf("the state has no %s inbound", protocol)
	return desiredstate.Inbound{}
}

// TestQuotaStartupReconcileKeepsQuickAccessCredential is issue 3 (a): the quick
// user-access feature's inbound used to live only inside its revision, so the
// startup reconcile compared the published state against a render that had never
// heard of it and stripped the user's credential.
func TestQuotaStartupReconcileKeepsQuickAccessCredential(t *testing.T) {
	f := newQuotaFixture(t)
	_, credential := f.addProtocolUser(t, "quick_startup_user", "", "socks5")
	f.publish(t)
	f.quickAccess(t, "socks5", "0.0.0.0", 1080)
	inbound := f.publishedInbound(t, "socks5")

	restarted := restartQuotaDeployer(t, f)
	restarted.recover(f.ctx)
	f.settle()
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("the startup reconcile created %d automatic revisions for an in-sync node", count)
	}
	published, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !stateHasCredential(published, credential) {
		t.Fatal("the startup reconcile dropped the quick-access credential")
	}
	if kept := stateInbound(t, published, "socks5"); !kept.Enabled || kept.ID != inbound.ID {
		t.Fatalf("the startup reconcile changed the quick-access inbound: %+v", kept)
	}
}

// TestQuotaRenewalKeepsQuickAccessInbound is issue 3 (b): a renewal is not an
// activity transition but still deploys, and that automatic revision used to
// remove the quick-access credential and disable its inbound.
func TestQuotaRenewalKeepsQuickAccessInbound(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addProtocolUser(t, "quick_renew_user", time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "socks5")
	f.publish(t)
	f.quickAccess(t, "socks5", "0.0.0.0", 1080)
	inbound := f.publishedInbound(t, "socks5")

	renewed := renewUser(t, f, account, time.Now().UTC().Add(72*time.Hour).Format(time.RFC3339))
	flowWait(t, "one automatic revision carrying the new expiry", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	state := f.state(t, automatic.ID)
	if !stateHasCredential(state, credential) {
		t.Fatal("the renewal dropped the quick-access credential")
	}
	kept := stateInbound(t, state, "socks5")
	if !kept.Enabled || kept.ID != inbound.ID || len(kept.CredentialIDs) != 1 || kept.CredentialIDs[0] != credential.ID {
		t.Fatalf("the renewal changed the quick-access inbound: %+v", kept)
	}
	if got := stateUser(t, state, account.ID).ExpiresAt; got != renewed {
		t.Fatalf("the renewal kept %q, want %q", got, renewed)
	}
}

// TestQuotaDisableAndReEnableKeepQuickAccessInbound is issue 3 (c): a disable
// must remove the credential and a re-enable must restore it on the same
// business-data inbound, not on a revision-only one.
func TestQuotaDisableAndReEnableKeepQuickAccessInbound(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addProtocolUser(t, "quick_cycle_user", "", "socks5")
	f.publish(t)
	f.quickAccess(t, "socks5", "0.0.0.0", 1080)
	inbound := f.publishedInbound(t, "socks5")

	account.Status = "disabled"
	saveUser(t, f, account, 0)
	flowWait(t, "the disable to remove the credential", func() bool { return len(f.autoRevisions(t)) == 1 })
	removed := f.autoRevisions(t)[0]
	state := f.state(t, removed.ID)
	if stateHasCredential(state, credential) {
		t.Fatal("the disabled credential survived")
	}
	if kept := stateInbound(t, state, "socks5"); kept.Enabled || kept.ID != inbound.ID {
		t.Fatalf("the disabled inbound: %+v", kept)
	}
	f.apply(t, removed.ID)

	account.Status = "active"
	saveUser(t, f, account, 0)
	flowWait(t, "the re-enable to restore the credential", func() bool { return len(f.autoRevisions(t)) == 2 })
	restored := f.autoRevisions(t)[1]
	state = f.state(t, restored.ID)
	if !stateHasCredential(state, credential) {
		t.Fatal("the re-enabled credential was not restored")
	}
	if kept := stateInbound(t, state, "socks5"); !kept.Enabled || kept.ID != inbound.ID || len(kept.CredentialIDs) != 1 || kept.CredentialIDs[0] != credential.ID {
		t.Fatalf("the restored inbound: %+v", kept)
	}
	// The re-enable is only allowed because the published revision recorded that
	// the Owner wants this inbound enabled.
	intent, err := f.app.deployments.InboundIntent(f.ctx, restored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !inboundIntentNames(intent, inbound.ID) {
		t.Fatalf("the automatic revision lost the owner-enabled inbound: %v", intent)
	}
}

// TestQuotaBystanderTransitionKeepsQuickAccessCredential is issue 3 (d): another
// user's over-quota transition must not touch the quick-access credential.
func TestQuotaBystanderTransitionKeepsQuickAccessCredential(t *testing.T) {
	f := newQuotaFixture(t)
	bystander, credential := f.addProtocolUser(t, "quick_bystander_user", "", "socks5")
	other, otherCredential := f.addUser(t, "quick_over_user", "")
	f.publish(t)
	f.quickAccess(t, "socks5", "0.0.0.0", 1080)
	inbound := f.publishedInbound(t, "socks5")

	setQuota(t, f.ctx, f, other.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{other.ID})
	flowWait(t, "the over-quota transition", func() bool { return len(f.autoRevisions(t)) == 1 })
	state := f.state(t, f.autoRevisions(t)[0].ID)
	if stateHasCredential(state, otherCredential) {
		t.Fatal("the over-quota credential survived")
	}
	if !stateHasCredential(state, credential) {
		t.Fatal("the over-quota transition stripped the bystander's quick-access credential")
	}
	if kept := stateInbound(t, state, "socks5"); !kept.Enabled || kept.ID != inbound.ID || !strings.Contains(strings.Join(kept.CredentialIDs, ","), credential.ID) {
		t.Fatalf("the bystander's inbound: %+v", kept)
	}
	// The bystander stays an authorized user of the revision.
	stateUser(t, state, bystander.ID)
}

// TestOwnerProxyRevisionKeepsQuickAccessInbound is issue 3 (e): the normal
// renderer must see the quick-access inbound too, so an Owner publication keeps
// it and the rest of the node's configuration instead of wiping it.
func TestOwnerProxyRevisionKeepsQuickAccessInbound(t *testing.T) {
	f := newQuotaFixture(t)
	_, credential := f.addProtocolUser(t, "quick_owner_user", "", "socks5")
	f.publish(t)
	f.quickAccess(t, "socks5", "0.0.0.0", 1080)
	inbound := f.publishedInbound(t, "socks5")

	response := f.owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": f.nodeID, "summary": "Owner publication"})
	if response.Code != 201 {
		t.Fatalf("owner revision: %d %s", response.Code, response.Body.String())
	}
	var revision deployment.Revision
	if err := json.Unmarshal(response.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	state := f.state(t, revision.ID)
	if !stateHasCredential(state, credential) {
		t.Fatal("the Owner publication dropped the quick-access credential")
	}
	kept := stateInbound(t, state, "socks5")
	if !kept.Enabled || kept.ID != inbound.ID || !strings.Contains(strings.Join(kept.CredentialIDs, ","), credential.ID) {
		t.Fatalf("the Owner publication changed the quick-access inbound: %+v", kept)
	}
	// The quick draft no longer replaces the rest of the node's configuration:
	// the VLESS entry inbound of the fixture is still there.
	if entry := stateInbound(t, state, "vless"); entry.ID == "" || entry.Port != 24443 {
		t.Fatalf("the Owner publication lost the node's other inbound: %+v", entry)
	}
}

// rollbackNode rolls a node back to a successfully applied revision through the
// real HTTP endpoint and reports the agent's success, so the panel records the
// old revision as the node's runtime state.
func (f *quotaFixture) rollbackNode(t *testing.T, revision int64) {
	t.Helper()
	response := f.owner.call("POST", "/api/v1/nodes/"+f.nodeID+"/rollback", map[string]any{"revision": revision})
	if response.Code != 202 {
		t.Fatalf("rollback: %d %s", response.Code, response.Body.String())
	}
	var command struct {
		CommandID string `json:"commandId"`
		Revision  int64  `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &command); err != nil {
		t.Fatal(err)
	}
	if err := f.app.deployments.Result(f.ctx, f.nodeID, &pb.ApplyResult{CommandId: command.CommandID, Revision: command.Revision, Success: true}); err != nil {
		t.Fatalf("rollback result: %v", err)
	}
}

// TestQuotaRollbackToPreDisableRevisionIsCorrected is issue 2: a rollback
// re-applies a stored revision verbatim, so rolling back past a disable used to
// restore the disabled user's credential and leave it there until a panel
// restart. The successful rollback now queues one reconciliation, and the
// revision it creates must not start a chain of revisions.
func TestQuotaRollbackToPreDisableRevisionIsCorrected(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "rollback_disable_user", "")
	baseline := f.publish(t)
	f.markRecovered()
	f.settle()

	account.Status = "disabled"
	saveUser(t, f, account, 0)
	flowWait(t, "the disable to remove the credential", func() bool { return len(f.autoRevisions(t)) == 1 })
	f.apply(t, f.autoRevisions(t)[0].ID)
	f.settle()

	f.rollbackNode(t, baseline)
	flowWait(t, "the corrective revision after the rollback", func() bool { return len(f.autoRevisions(t)) == 2 })
	corrective := f.autoRevisions(t)[1]
	if stateHasCredential(f.state(t, corrective.ID), credential) {
		t.Fatal("the rollback restored the disabled user's credential and nothing corrected it")
	}
	if !strings.Contains(corrective.Summary, "post-rollback") {
		t.Fatalf("corrective summary: %q", corrective.Summary)
	}

	// The corrective revision's own success must not queue another one.
	f.apply(t, corrective.ID)
	f.settle()
	if count := len(f.autoRevisions(t)); count != 2 {
		t.Fatalf("the corrected node produced %d automatic revisions", count)
	}
}

// TestQuotaStaleDraftDeployIsCorrected is issue 2 on the deploy path: a draft
// rendered before a disable is still deployable, and applying it used to leave
// the disabled user's credential on the node.
func TestQuotaStaleDraftDeployIsCorrected(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "stale_draft_user", "")
	f.publish(t)
	f.markRecovered()
	response := f.owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": f.nodeID, "summary": "Stale draft"})
	if response.Code != 201 {
		t.Fatalf("stale draft: %d %s", response.Code, response.Body.String())
	}
	var stale deployment.Revision
	if err := json.Unmarshal(response.Body.Bytes(), &stale); err != nil {
		t.Fatal(err)
	}
	if !stateHasCredential(f.state(t, stale.ID), credential) {
		t.Fatal("the stale draft must still carry the credential for this test to mean anything")
	}
	f.settle()

	account.Status = "disabled"
	saveUser(t, f, account, 0)
	flowWait(t, "the disable to remove the credential", func() bool { return len(f.autoRevisions(t)) == 1 })
	f.apply(t, f.autoRevisions(t)[0].ID)
	f.settle()

	if deploy := f.owner.call("POST", "/api/v1/revisions/"+strconv.FormatInt(stale.ID, 10)+"/deploy", nil); deploy.Code != 202 {
		t.Fatalf("stale deploy: %d %s", deploy.Code, deploy.Body.String())
	}
	f.apply(t, stale.ID)
	flowWait(t, "the corrective revision after the stale deploy", func() bool { return len(f.autoRevisions(t)) == 2 })
	corrective := f.autoRevisions(t)[1]
	if stateHasCredential(f.state(t, corrective.ID), credential) {
		t.Fatal("the stale deploy restored the disabled user's credential and nothing corrected it")
	}
	if !strings.Contains(corrective.Summary, "post-deploy") {
		t.Fatalf("corrective summary: %q", corrective.Summary)
	}
	f.apply(t, corrective.ID)
	f.settle()
	if count := len(f.autoRevisions(t)); count != 2 {
		t.Fatalf("the corrected node produced %d automatic revisions", count)
	}
}

// TestQuotaRollbackToPreEnableRevisionRestoresCredential is the reverse of issue
// 2: rolling back to a revision rendered while the user was disabled must not
// lock a re-enabled user out.
func TestQuotaRollbackToPreEnableRevisionRestoresCredential(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "rollback_enable_user", "")
	account.Status = "disabled"
	saveUser(t, f, account, 0)
	baseline := f.publish(t)
	f.markRecovered()
	f.settle()
	if stateHasCredential(f.state(t, baseline), credential) {
		t.Fatal("the baseline must not carry a disabled user's credential")
	}

	account.Status = "active"
	saveUser(t, f, account, 0)
	flowWait(t, "the re-enable to restore the credential", func() bool { return len(f.autoRevisions(t)) == 1 })
	f.apply(t, f.autoRevisions(t)[0].ID)
	f.settle()

	f.rollbackNode(t, baseline)
	flowWait(t, "the corrective revision after the rollback", func() bool { return len(f.autoRevisions(t)) == 2 })
	corrective := f.autoRevisions(t)[1]
	if !stateHasCredential(f.state(t, corrective.ID), credential) {
		t.Fatal("the rollback locked the re-enabled user out and nothing corrected it")
	}
	if !strings.Contains(corrective.Summary, "post-rollback") {
		t.Fatalf("corrective summary: %q", corrective.Summary)
	}
}

// TestQuotaDeletedUserRevokedOnNode is the field case of 2026-10-25: deleting a
// proxy user removed the rows (with credential tombstones for the counters), but
// the nodes kept accepting the credential until the Owner republished by hand.
// Deleting is now a revocation like disabling: one automatic revision that no
// longer carries the credential.
func TestQuotaDeletedUserRevokedOnNode(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "deleted_user", "")
	f.publish(t)
	alerts := f.recordAlerts()

	if response := f.owner.call("DELETE", "/api/v1/users/"+account.ID, nil); response.Code != 204 {
		t.Fatalf("delete user: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "one automatic revision removing the deleted credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	if automatic.NodeID != f.nodeID {
		t.Fatalf("automatic revision targeted %q", automatic.NodeID)
	}
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("the deleted credential survived in the automatic revision")
	}
	if !strings.Contains(automatic.Summary, "deleted_user deleted") {
		t.Fatalf("summary %q does not name the deleted user", automatic.Summary)
	}
	if reasons := alerts.reasons(); len(reasons) != 1 || reasons[0] != "deleted" {
		t.Fatalf("alerts: %v", reasons)
	}

	// Applying the revocation settles the node: the post-deploy reconcile finds
	// it in sync and creates nothing, and no later tick resurrects the user.
	f.apply(t, automatic.ID)
	f.idle(t)
	f.markRecovered()
	f.app.quotaDeployer.tickOnce(f.ctx)
	f.idle(t)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("the applied revocation produced %d automatic revisions", count)
	}
}

// TestQuotaDeletedDeviceRevokesOnlyThatDevice: deleting one device must take
// only that device's credential off the node, not the user's other device.
func TestQuotaDeletedDeviceRevokesOnlyThatDevice(t *testing.T) {
	f := newQuotaFixture(t)
	account, first := f.addUser(t, "device_user", "")
	account.MaxDevices = 2
	saveUser(t, f, account, 0)
	response := f.owner.call("POST", "/api/v1/users/"+account.ID+"/devices", map[string]any{"name": "second device", "protocols": []string{"vless"}})
	if response.Code != 201 {
		t.Fatalf("second device: %d %s", response.Code, response.Body.String())
	}
	var second user.Device
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Credentials) != 1 || second.Credentials[0].Protocol != "vless" {
		t.Fatalf("second device credentials: %v", second.Credentials)
	}
	kept := second.Credentials[0]
	f.publish(t)
	alerts := f.recordAlerts()

	var firstDeviceID string
	if err := f.app.Database.QueryRowContext(f.ctx, "SELECT device_id FROM credentials WHERE id=?", first.ID).Scan(&firstDeviceID); err != nil {
		t.Fatal(err)
	}
	if response := f.owner.call("DELETE", "/api/v1/devices/"+firstDeviceID, nil); response.Code != 204 {
		t.Fatalf("delete device: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "one automatic revision removing the deleted device's credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	revoked := f.state(t, automatic.ID)
	if stateHasCredential(revoked, first) {
		t.Fatal("the deleted device's credential survived in the automatic revision")
	}
	if !stateHasCredential(revoked, kept) {
		t.Fatal("the surviving device's credential was removed with the deleted one")
	}
	if !strings.Contains(automatic.Summary, "device deleted") {
		t.Fatalf("summary %q does not mention the deleted device", automatic.Summary)
	}
	if reasons := alerts.reasons(); len(reasons) != 1 || reasons[0] != "device deleted" {
		t.Fatalf("alerts: %v", reasons)
	}

	f.apply(t, automatic.ID)
	f.idle(t)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("the applied revocation produced %d automatic revisions", count)
	}
}

// TestQuotaDeletedDeviceAfterInvalidDraftRevokesIt is the same delete while the
// Owner's saved draft cannot be rendered: every automatic revision is then built
// from the published state alone, and the deleted device's credential is gone
// from the credentials table while its published entry still names the active
// user. Without the credential-owner check that fallback found nothing to change
// and the credential stayed valid on the node forever.
func TestQuotaDeletedDeviceAfterInvalidDraftRevokesIt(t *testing.T) {
	f := newQuotaFixture(t)
	account, first := f.addUser(t, "fallback_device_user", "")
	account.MaxDevices = 2
	saveUser(t, f, account, 0)
	response := f.owner.call("POST", "/api/v1/users/"+account.ID+"/devices", map[string]any{"name": "second device", "protocols": []string{"vless"}})
	if response.Code != 201 {
		t.Fatalf("second device: %d %s", response.Code, response.Body.String())
	}
	var second user.Device
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Credentials) != 1 {
		t.Fatalf("second device credentials: %v", second.Credentials)
	}
	kept := second.Credentials[0]
	f.publish(t)
	flaky := &quotaFlakySnapshotter{inner: f.app.quotaDeployer.proxy, fail: true}
	f.app.quotaDeployer.proxy = flaky

	var firstDeviceID string
	if err := f.app.Database.QueryRowContext(f.ctx, "SELECT device_id FROM credentials WHERE id=?", first.ID).Scan(&firstDeviceID); err != nil {
		t.Fatal(err)
	}
	if response := f.owner.call("DELETE", "/api/v1/devices/"+firstDeviceID, nil); response.Code != 204 {
		t.Fatalf("delete device: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "one automatic revision removing the deleted device's credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	revoked := f.state(t, automatic.ID)
	if stateHasCredential(revoked, first) {
		t.Fatal("the deleted device's credential survived the published-only revision")
	}
	if !stateHasCredential(revoked, kept) {
		t.Fatal("the surviving device's credential was removed with the deleted one")
	}
	if !strings.Contains(automatic.Summary, "device deleted") {
		t.Fatalf("summary %q does not mention the deleted device", automatic.Summary)
	}

	// Once the draft renders again the regular render takes over and finds the
	// node in sync, so the fix costs no further revision.
	flaky.setFail(false)
	f.apply(t, automatic.ID)
	f.idle(t)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("the applied revocation produced %d automatic revisions", count)
	}
}

// field report: the Owner removed the user's node group grant without
// publishing, so the node still runs the credential while no granted group
// contains the user. Deleting the user has to reach that node, which only the
// published-state scan of schedule can find.
func TestQuotaDeletedUserAfterRemovedGrantReachesNode(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "deleted_after_grant_user", "")
	f.publish(t)
	account.NodeGroupIDs = []string{}
	saveUser(t, f, account, 0)
	published, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !stateHasCredential(published, credential) {
		t.Fatal("the node must still run the credential the grant removal did not publish")
	}
	f.markRecovered()

	if response := f.owner.call("DELETE", "/api/v1/users/"+account.ID, nil); response.Code != 204 {
		t.Fatalf("delete user: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "one automatic revision removing the deleted credential", func() bool {
		return len(f.autoRevisions(t)) == 1
	})
	automatic := f.autoRevisions(t)[0]
	if stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("the deleted credential survived on the node that still ran it")
	}
	if !strings.Contains(automatic.Summary, "deleted") {
		t.Fatalf("summary %q does not mention the deletion", automatic.Summary)
	}
}

// TestQuotaDeletedUserWithoutPublishedCredentialDoesNothing: a user no node ever
// received a credential for leaves every published state untouched, so the
// delete must not create a revision for them.
func TestQuotaDeletedUserWithoutPublishedCredentialDoesNothing(t *testing.T) {
	f := newQuotaFixture(t)
	response := f.owner.call("POST", "/api/v1/users", user.Input{Username: "ungranted_user", DisplayName: "ungranted_user", Status: "active", MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{"vless"}})
	if response.Code != 201 {
		t.Fatalf("user: %d %s", response.Code, response.Body.String())
	}
	var account user.User
	if err := json.Unmarshal(response.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	if device := f.owner.call("POST", "/api/v1/users/"+account.ID+"/devices", map[string]any{"name": "ungranted device", "protocols": []string{"vless"}}); device.Code != 201 {
		t.Fatalf("device: %d %s", device.Code, device.Body.String())
	}
	baseline := f.publish(t)
	f.markRecovered()
	alerts := f.recordAlerts()

	if response := f.owner.call("DELETE", "/api/v1/users/"+account.ID, nil); response.Code != 204 {
		t.Fatalf("delete user: %d %s", response.Code, response.Body.String())
	}
	flowWait(t, "the delete alert", func() bool { return len(alerts.reasons()) == 1 })
	if reasons := alerts.reasons(); reasons[0] != "deleted" {
		t.Fatalf("alerts: %v", reasons)
	}
	// Nothing may be queued for the node at all, and its published state must
	// still be the baseline.
	f.idle(t)
	f.settle()
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("the delete of an unpublished user produced %d automatic revisions", count)
	}
	published, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Revision != baseline {
		t.Fatalf("the node moved from revision %d to %d", baseline, published.Revision)
	}
}
