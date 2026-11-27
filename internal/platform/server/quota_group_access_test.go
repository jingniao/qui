package server

import (
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestQuotaMergeDoesNotRetainBindingToRemovedInbound(t *testing.T) {
	t.Parallel()
	c := desiredstate.Credential{ID: "shared", UserID: "u", Protocol: "vless"}
	a := desiredstate.Inbound{ID: "a", Protocol: "vless", Enabled: true, CredentialIDs: []string{c.ID}}
	b := a
	b.ID = "b"
	published := desiredstate.State{Credentials: []desiredstate.Credential{c}, Inbounds: []desiredstate.Inbound{a, b}}
	fresh := published
	fresh.Inbounds = []desiredstate.Inbound{b}
	next := mergeUserAccess(published, fresh, []string{"a", "b"})
	if len(next.Inbounds[0].CredentialIDs) != 0 || next.Inbounds[0].Enabled || len(next.Inbounds[1].CredentialIDs) != 1 {
		t.Fatal("removed inbound kept a shared credential", next.Inbounds)
	}
}

func TestPublishedFallbackKeepsRelayOnlyCredential(t *testing.T) {
	f := newQuotaFixture(t)
	_, credential := f.addUser(t, "relay_only", "")
	state := f.state(t, f.publish(t))
	inbound := state.Inbounds[0].ID
	if _, err := f.app.Database.ExecContext(f.ctx, `INSERT INTO tunnel_rules
		(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_host,target_port,subscription_visible,enabled,status,published_version,created_at,updated_at,published_snapshot)
		VALUES('relay-fallback','Relay',?,34567,'manual','tcp','manual','example.com',443,0,1,'modified',1,'now','now',json_object('InboundID',?))`, f.nodeID, inbound); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Database.ExecContext(f.ctx, "INSERT INTO node_group_relays(group_id,rule_id) VALUES(?,'relay-fallback')", f.groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Database.ExecContext(f.ctx, "DELETE FROM node_group_inbounds WHERE group_id=?", f.groupID); err != nil {
		t.Fatal(err)
	}
	next, err := f.app.quotaDeployer.filterPublishedGroupAccess(f.ctx, state)
	if err != nil || !stateHasCredential(next, credential) || !next.Inbounds[0].Enabled {
		t.Fatalf("fallback removed the published relay credential: %v", err)
	}
}
