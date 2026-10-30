package node

import (
	"context"
	"testing"
)

func TestGroupEntriesRepositoryAndDerivedServers(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	for _, q := range []string{
		"INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('a','A',?,'socks5','0.0.0.0',10001,1,1,0,'direct','[]',x'01','now')",
		"INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('b','B',?,'socks5','0.0.0.0',10002,1,1,0,'direct','[]',x'01','now')",
	} {
		if _, err := r.Database.Exec(q, id); err != nil {
			t.Fatal(err)
		}
	}
	input := GroupInput{Name: "entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{"a"}}
	g, err := r.SaveGroup(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.NodeIDs) != 1 || g.NodeIDs[0] != id || len(g.InboundIDs) != 1 || g.Imports == nil || g.GrantedUserIDs == nil {
		t.Fatal(g)
	}
	input.ServerIDs = []string{id}
	input.InboundIDs = []string{"a", "b"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Database.Exec("DELETE FROM inbounds WHERE id='a'"); err != nil {
		t.Fatal(err)
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.NodeIDs) != 1 || len(g.InboundIDs) != 1 {
		t.Fatal(g, err)
	}
	if _, err = r.Database.Exec("DELETE FROM inbounds WHERE id='b'"); err != nil {
		t.Fatal(err)
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.NodeIDs) != 1 || len(g.ServerIDs) != 1 || len(g.InboundIDs) != 0 {
		t.Fatal("inbound cascade removed explicit server", g, err)
	}
	if _, err = r.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('b','B',?,'socks5','0.0.0.0',10002,1,1,0,'direct','[]',x'01','now')", id); err != nil {
		t.Fatal(err)
	}
	input.ServerIDs = []string{}
	input.InboundIDs = []string{}
	input.NodeIDs = []string{id} // Explicit entries override legacy servers.
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.NodeIDs) != 0 {
		t.Fatal(g, err)
	}
	input.InboundIDs = nil
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.InboundIDs) != 1 {
		t.Fatal(g, err)
	}
	input.InboundIDs = []string{"unknown"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("unknown inbound accepted")
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.InboundIDs) != 1 {
		t.Fatal("failed save changed membership", g, err)
	}
	input.InboundIDs = []string{"b", "b"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestExplicitServersUnionLegacyAndDeletion(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "explicit")
	input := GroupInput{Name: "servers", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, ServerIDs: []string{n.ID}}
	g, err := r.SaveGroup(ctx, "", input)
	if err != nil || len(g.ServerIDs) != 1 || len(g.NodeIDs) != 1 || len(g.InboundIDs) != 0 {
		t.Fatal(g, err)
	}
	// Explicit-only membership is a policy scope, never an inbound grant.
	groups, err := r.GroupIDsForNode(ctx, n.ID)
	if err != nil || len(groups) != 1 || groups[0] != g.ID {
		t.Fatal(groups, err)
	}
	input.ServerIDs = nil
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.ServerIDs) != 1 {
		t.Fatal("omitted explicit servers lost", g, err)
	}
	input.NodeIDs = []string{} // Exact payload shape of bulk leave.
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.NodeIDs) != 0 {
		t.Fatal("bulk leave retained explicit server", g, err)
	}
	for _, purpose := range []string{"relay", "egress"} {
		input.Purpose = purpose
		input.NodeIDs = []string{n.ID}
		g, err = r.SaveGroup(ctx, g.ID, input)
		if err != nil || len(g.ServerIDs) != 1 || len(g.InboundIDs) != 0 || g.SubscriptionVisible {
			t.Fatal(g, err)
		}
	}
	input.ServerIDs = []string{"missing"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("unknown server accepted")
	}
	input.ServerIDs = []string{n.ID, n.ID}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("duplicate server accepted")
	}
	if err = r.Delete(ctx, n.ID, nil, nil, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.ServerIDs) != 0 || len(g.NodeIDs) != 0 {
		t.Fatal("logical deletion retained server", g, err)
	}
}
