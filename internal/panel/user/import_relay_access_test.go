package user

import (
	"context"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
)

func TestAccessForGroupsImportRelayHasNoManagedInbound(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	u, err := r.Save(ctx, "", userInput())
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: r.Database, Vault: r.Vault}
	n, _, _, err := nodes.CreateEnrollment(ctx, "import relay entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, `INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,subscription_visible,enabled,status,created_at,updated_at)
VALUES('import-rule','import relay',?,23001,'manual','both','import',1,1,'draft','now','now')`, n.ID); err != nil {
		t.Fatal(err)
	}
	g, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "import relay only", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, RelayIDs: []string{"import-rule"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO user_node_group_grants(user_id,group_id) VALUES(?,?)", u.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	access, err := r.AccessForGroups(ctx, []string{g.ID})
	if err != nil || len(access) != 1 || len(access[0].InboundIDs) != 0 {
		t.Fatalf("import relay introduced managed inbound access: %+v, %v", access, err)
	}
}
