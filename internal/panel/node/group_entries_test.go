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
