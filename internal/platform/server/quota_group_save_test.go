package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
)

// saveGroup writes the fixture's node group and returns the response, so a test
// can assert both the status code and what the save queued.
func (f *quotaFixture) saveGroup(t *testing.T, input node.GroupInput) *httptest.ResponseRecorder {
	t.Helper()
	return f.owner.call("PUT", "/api/v1/node-groups/"+f.groupID, input)
}

// groupInput is the fixture's group with the given membership: purpose
// subscription, enabled and subscription visible, like the fixture creates it.
func (f *quotaFixture) groupInput(inboundIDs ...string) node.GroupInput {
	if inboundIDs == nil {
		inboundIDs = []string{}
	}
	return node.GroupInput{Name: "Quota Entrances", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: inboundIDs}
}

// inboundID looks an inbound of the fixture's node up by name.
func (f *quotaFixture) inboundID(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := f.app.Database.QueryRowContext(f.ctx, "SELECT id FROM inbounds WHERE node_id=? AND name=?", f.nodeID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// queued reports whether the deployer holds any work for the fixture's node.
func (f *quotaFixture) queued() bool {
	deployer := f.app.quotaDeployer
	deployer.mu.Lock()
	defer deployer.mu.Unlock()
	return deployer.pending[f.nodeID] != nil || deployer.timers[f.nodeID] != nil
}

// groupInbound creates an inbound on the fixture's node that no group holds, so
// a later group save is what puts the user's credential on it. The fixture's own
// inbound is already in the group.
func (f *quotaFixture) groupInbound(t *testing.T) string {
	t.Helper()
	response := f.owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "Group Inbound", NodeID: f.nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: 24555, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if response.Code != 201 {
		t.Fatalf("group inbound: %d %s", response.Code, response.Body.String())
	}
	return f.inboundID(t, "Group Inbound")
}

// inboundBinds reports whether the state runs the inbound with this ID and binds
// the user's credential to it. The credential list alone proves nothing: an
// automatic revision can list a fresh user credential in state.Credentials while
// no inbound carries it, so a group-membership assertion has to read the
// inbound's own CredentialIDs.
func inboundBinds(state desiredstate.State, inboundID string, credential user.Credential) bool {
	for _, inbound := range state.Inbounds {
		if inbound.ID != inboundID {
			continue
		}
		for _, id := range inbound.CredentialIDs {
			if id == credential.ID {
				return true
			}
		}
	}
	return false
}

// stateHasInbound reports whether the state lists the inbound at all, whatever
// it binds.
func stateHasInbound(state desiredstate.State, inboundID string) bool {
	for _, inbound := range state.Inbounds {
		if inbound.ID == inboundID {
			return true
		}
	}
	return false
}

// automaticRevision waits until the fixture's node has exactly count automatic
// revisions and returns the newest one, so a test can assert how many revisions a
// change produced as well as what the last one carries.
func (f *quotaFixture) automaticRevision(t *testing.T, count int, what string) deployment.Revision {
	t.Helper()
	flowWait(t, what, func() bool { return len(f.autoRevisions(t)) == count })
	return f.autoRevisions(t)[count-1]
}

// grantedGroupState builds the state every group-change test starts from. The
// fixture's group already holds "Quota VLESS" and the user is granted it, so the
// baseline publication runs that inbound with the user's credential. A second
// inbound is then created outside the group and published, which leaves the node
// running it disabled and without credentials - only the group save may put the
// credential on it. The save is asserted to bind both inbounds, as the automatic
// revision keeps the published inbounds and rebinds credentials per inbound from
// the fresh draft render. Returns the added inbound's ID and the credential the
// group grants.
func (f *quotaFixture) grantedGroupState(t *testing.T, username string) (string, user.Credential) {
	t.Helper()
	_, credential := f.addUser(t, username, "")
	quota := f.inboundID(t, "Quota VLESS")
	added := f.groupInbound(t)
	f.publish(t)
	if response := f.saveGroup(t, f.groupInput(quota, added)); response.Code != 200 {
		t.Fatalf("save group: %d %s", response.Code, response.Body.String())
	}
	automatic := f.automaticRevision(t, 1, "an automatic revision for the group's new member")
	state := f.state(t, automatic.ID)
	if !inboundBinds(state, added, credential) {
		t.Fatalf("the automatic revision did not bind the granted credential to the new group member %s", added)
	}
	if !inboundBinds(state, quota, credential) {
		t.Fatalf("the automatic revision dropped the credential the group already granted on %s", quota)
	}
	f.apply(t, automatic.ID)
	f.idle(t)
	return added, credential
}

// TestSavedNodeGroupDeploysTheGrantedCredential is plan 1: adding a published
// inbound to a group that already has granted users must put their credentials
// on the nodes, instead of waiting for a hand-made publication. The helper
// asserts the bindings on both group members; what is left is that the applied
// revision settled the node and needed no correction of its own.
func TestSavedNodeGroupDeploysTheGrantedCredential(t *testing.T) {
	f := newQuotaFixture(t)
	f.grantedGroupState(t, "group_save_user")
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("the saved group left %d automatic revisions, want exactly one", count)
	}
}

// TestNodeGroupChangeRemovesTheGrantedCredential is the other direction: the
// four ways a group stops giving a user access all have to take the credential
// off the node the group published it to. Every case starts from the positive
// state, so the node runs the credential on both members before the change and a
// single further automatic revision is what takes it away again.
func TestNodeGroupChangeRemovesTheGrantedCredential(t *testing.T) {
	cases := []struct {
		name string
		// keepsOtherMember says whether "Quota VLESS" may keep the credential:
		// only removing the added member from the group leaves it granted.
		keepsOtherMember bool
		change           func(t *testing.T, f *quotaFixture, quotaInboundID, addedInboundID string)
	}{
		{"inbound removed", true, func(t *testing.T, f *quotaFixture, quotaInboundID, addedInboundID string) {
			if response := f.saveGroup(t, f.groupInput(quotaInboundID)); response.Code != 200 {
				t.Fatalf("save group: %d %s", response.Code, response.Body.String())
			}
		}},
		{"group disabled", false, func(t *testing.T, f *quotaFixture, quotaInboundID, addedInboundID string) {
			input := f.groupInput(quotaInboundID, addedInboundID)
			input.Enabled = false
			if response := f.saveGroup(t, input); response.Code != 200 {
				t.Fatalf("save group: %d %s", response.Code, response.Body.String())
			}
		}},
		{"purpose relay", false, func(t *testing.T, f *quotaFixture, quotaInboundID, addedInboundID string) {
			input := f.groupInput(quotaInboundID, addedInboundID)
			input.Purpose = "relay"
			if response := f.saveGroup(t, input); response.Code != 200 {
				t.Fatalf("save group: %d %s", response.Code, response.Body.String())
			}
		}},
		{"group deleted", false, func(t *testing.T, f *quotaFixture, quotaInboundID, addedInboundID string) {
			if response := f.owner.call("DELETE", "/api/v1/node-groups/"+f.groupID, nil); response.Code != 204 {
				t.Fatalf("delete group: %d %s", response.Code, response.Body.String())
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newQuotaFixture(t)
			added, credential := f.grantedGroupState(t, "group_change_user")
			quota := f.inboundID(t, "Quota VLESS")

			testCase.change(t, f, quota, added)
			automatic := f.automaticRevision(t, 2, "an automatic revision for the group change")
			state := f.state(t, automatic.ID)
			if inboundBinds(state, added, credential) {
				t.Fatal("the group change left the credential on an inbound the group no longer reaches")
			}
			if kept := inboundBinds(state, quota, credential); kept != testCase.keepsOtherMember {
				t.Fatalf("the group's other inbound binds the credential = %v, want %v", kept, testCase.keepsOtherMember)
			}
		})
	}
}

// TestNodeGroupRenameOnlyDeploysNothing saves a group whose membership and
// flags are unchanged: the reconciliation finds the node in sync and creates no
// revision.
func TestNodeGroupRenameOnlyDeploysNothing(t *testing.T) {
	f := newQuotaFixture(t)
	added, _ := f.grantedGroupState(t, "group_rename_user")
	input := f.groupInput(f.inboundID(t, "Quota VLESS"), added)
	input.Name, input.Remark = "Renamed Entrances", "only the label changed"
	if response := f.saveGroup(t, input); response.Code != 200 {
		t.Fatalf("save group: %d %s", response.Code, response.Body.String())
	}
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 1 {
		t.Fatalf("a renamed group created %d extra automatic revisions", count-1)
	}
}

// TestNodeGroupWithoutGrantsSchedulesNothing proves the hook is skipped when the
// group has no granted users: there is nobody whose access could change.
func TestNodeGroupWithoutGrantsSchedulesNothing(t *testing.T) {
	f := newQuotaFixture(t)
	f.publish(t)
	if response := f.saveGroup(t, f.groupInput(f.inboundID(t, "Quota VLESS"), f.groupInbound(t))); response.Code != 200 {
		t.Fatalf("save group: %d %s", response.Code, response.Body.String())
	}
	if f.queued() {
		t.Fatal("a group with no granted users queued a reconciliation")
	}
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("a group with no granted users created %d automatic revisions", count)
	}
}

// TestFailedNodeGroupSaveSchedulesNothing proves a rejected save is not a
// change: neither the 400 nor the 404 may queue a reconciliation.
func TestFailedNodeGroupSaveSchedulesNothing(t *testing.T) {
	f := newQuotaFixture(t)
	f.addUser(t, "group_failed_save_user", "")
	f.publish(t)

	invalid := f.groupInput(f.inboundID(t, "Quota VLESS"))
	invalid.Name = ""
	if response := f.saveGroup(t, invalid); response.Code != 400 {
		t.Fatalf("invalid save: %d %s", response.Code, response.Body.String())
	}
	if response := f.saveGroup(t, f.groupInput("missing-inbound")); response.Code != 400 {
		t.Fatalf("unknown inbound: %d %s", response.Code, response.Body.String())
	}
	if response := f.owner.call("PUT", "/api/v1/node-groups/missing-group", f.groupInput(f.inboundID(t, "Quota VLESS"))); response.Code != 404 {
		t.Fatalf("unknown group: %d %s", response.Code, response.Body.String())
	}
	if f.queued() {
		t.Fatal("a rejected save queued a reconciliation")
	}
	time.Sleep(3 * f.app.quotaDeployer.window)
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("a rejected save created %d automatic revisions", count)
	}
}

// TestUnpublishedInboundInGroupIsNotCarried is the other half of the same hook: a
// group may list an inbound the Owner saved but never published. A group save is
// not a publication, so the automatic revision - which is built from the
// published state and only borrows the fresh render's per-inbound bindings - must
// not start serving that inbound on the node.
func TestUnpublishedInboundInGroupIsNotCarried(t *testing.T) {
	f := newQuotaFixture(t)
	_, credential := f.addUser(t, "group_unpublished_user", "")
	f.publish(t)
	quota := f.inboundID(t, "Quota VLESS")
	unpublished := f.groupInbound(t)
	if response := f.saveGroup(t, f.groupInput(quota, unpublished)); response.Code != 200 {
		t.Fatalf("save group: %d %s", response.Code, response.Body.String())
	}
	time.Sleep(3 * f.app.quotaDeployer.window)

	// The strictest statement that holds by construction: an automatic revision
	// only ever carries the inbounds of the published state, so no revision may
	// list the unpublished inbound. The assertion reads the inbound and not the
	// credential because mergeUserAccess appends every fresh user credential to
	// state.Credentials even when no inbound binds it - a credential's presence
	// there would prove nothing about what the node serves.
	for _, automatic := range f.autoRevisions(t) {
		if stateHasInbound(f.state(t, automatic.ID), unpublished) {
			t.Fatalf("automatic revision %d carries the unpublished inbound %s", automatic.ID, unpublished)
		}
	}
	// Beyond that invariant, the merge finds nothing to change at all: the group
	// save must not have produced a revision, since any revision would mean the
	// save had published something the Owner had not.
	if count := len(f.autoRevisions(t)); count != 0 {
		t.Fatalf("a group entry for an unpublished inbound created %d automatic revisions", count)
	}
	// The node therefore still runs exactly what the baseline published.
	published, err := f.app.deployments.Published(f.ctx, f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !inboundBinds(published, quota, credential) {
		t.Fatal("the group save took the credential off the inbound the node already served")
	}
	if stateHasInbound(published, unpublished) {
		t.Fatalf("the group save published the unpublished inbound %s", unpublished)
	}
}
