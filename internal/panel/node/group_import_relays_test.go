package node

import (
	"path/filepath"
	"testing"
)

func TestGroupImportRelayDoesNotGrantManagedCredentials(t *testing.T) {
	r, ctx := groupImportRepository(t)
	n := enrolledNode(t, r, ctx, "forward entry")
	insert(t, r, ctx, `INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_import_id,subscription_visible,enabled,status,created_at,updated_at)
VALUES('import-rule','import relay',?,23001,'manual','both','import','stable-one',1,1,'draft','now','now')`, n.ID)
	g, err := r.SaveGroup(ctx, "", GroupInput{Name: "import relay group", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, RelayIDs: []string{"import-rule"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.RelayIDs) != 1 || g.RelayIDs[0] != "import-rule" || len(g.RelayNodeIDs) != 0 || len(g.NodeIDs) != 0 {
		t.Fatalf("import relay introduced managed servers: %+v", g)
	}
	insert(t, r, ctx, `INSERT INTO users(id,username,display_name,status,max_devices,max_connections,protocols_json,created_at) VALUES('relay-user','relay-user','user','active',1,8,'["socks5"]','now')`)
	insert(t, r, ctx, "INSERT INTO user_node_group_grants(user_id,group_id) VALUES('relay-user',?)", g.ID)
	groups, err := r.SubscriptionGroups(ctx, n.ID, nil)
	if err != nil || len(groups) != 0 {
		t.Fatalf("import relay granted a managed node: %v, %v", groups, err)
	}
	relayGroups, err := r.UserRelayGroups(ctx, "import-rule", "relay-user")
	if err != nil || len(relayGroups) != 0 {
		t.Fatalf("import relay granted inbound credentials: %v, %v", relayGroups, err)
	}
	// Restore keeps today's rule members even when their target is imported.
	archive := filepath.Join(t.TempDir(), "current-groups.db")
	insert(t, r, ctx, "VACUUM INTO ?", archive)
	insert(t, r, ctx, "DELETE FROM node_group_relays")
	insert(t, r, ctx, "ATTACH DATABASE ? AS restore_source", archive)
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := r.PrepareGroupRestoreTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := r.Group(ctx, g.ID)
	if err != nil || len(got.RelayIDs) != 1 || got.RelayIDs[0] != "import-rule" {
		t.Fatalf("restore lost import relay: %+v, %v", got, err)
	}
}
