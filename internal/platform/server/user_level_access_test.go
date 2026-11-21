package server

import (
	"context"
	"encoding/json"

	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
)

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
	t.Parallel()
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
	groupResponse := f.owner.call("POST", "/api/v1/node-groups", node.GroupInput{Name: "Second Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: func() []string {
		selected := []string{second.ID}
		ids := []string{}
		for _, serverID := range selected {
			rows, err := f.app.Database.Query("SELECT id FROM inbounds WHERE node_id=? AND subscription_visible=1 AND internal_relay=0 ORDER BY id", serverID)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var inboundID string
				if err := rows.Scan(&inboundID); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				ids = append(ids, inboundID)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			rows.Close()
		}
		return ids
	}()})
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
	if r := f.owner.call("PUT", "/api/v1/node-groups/"+secondGroup.ID, node.GroupInput{Name: secondGroup.Name, Purpose: secondGroup.Purpose, Enabled: true, SubscriptionVisible: true, InboundIDs: func() []string {
		selected := []string{second.ID}
		ids := []string{}
		for _, serverID := range selected {
			rows, err := f.app.Database.Query("SELECT id FROM inbounds WHERE node_id=? AND subscription_visible=1 AND internal_relay=0 ORDER BY id", serverID)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var inboundID string
				if err := rows.Scan(&inboundID); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				ids = append(ids, inboundID)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			rows.Close()
		}
		return ids
	}()}); r.Code != 200 {
		t.Fatal(r.Body.String())
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
	if err != nil || len(credentials) != len(user.AllProtocols) {
		t.Fatalf("credentials: %v %v", err, credentials)
	}
	var credential user.Credential
	for _, c := range credentials {
		if c.Protocol == "vless" && c.Secret != "" {
			credential = c
			break
		}
	}
	if credential.Secret == "" {
		t.Fatalf("missing vless credential in %v", credentials)
	}
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

// A5.6: allowing one more protocol adds its credential with the next automatic
// revision, and resetting the credentials revokes the old secrets on the node. The
// fixture's entry inbound is VLESS, so the second protocol is SOCKS5, which the
// quick user-access draft creates; the property under test is the same one the
// plan names for socks5 -> trojan.
func TestProtocolChangeAddsAndResetRevokesCredentials(t *testing.T) {
	t.Parallel()
	f := newQuotaFixture(t)
	account, vlessCredential := f.addProtocolUser(t, "protocol_user", "", "vless")
	// Simulate an older user that was created before v1.5 with only VLESS:
	f.app.Database.ExecContext(f.ctx, `DELETE FROM credentials WHERE user_id=? AND protocol='socks5'`, account.ID)
	f.app.Database.ExecContext(f.ctx, `UPDATE users SET protocols_json='["vless"]' WHERE id=?`, account.ID)
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
