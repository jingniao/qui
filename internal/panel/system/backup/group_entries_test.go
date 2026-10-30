package backup

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/importer"
	"testing"
)

func TestGroupEntriesBackupRoundTrip(t *testing.T) {
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	var server string
	if err := s.Database.QueryRow("SELECT id FROM nodes LIMIT 1").Scan(&server); err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: s.Database}
	inbound, err := (&proxy.Service{Repository: &proxy.Repository{Database: s.Database, Vault: s.ConfigurationVault}, Nodes: nodes}).SaveInbound(ctx, "", proxy.InboundInput{Name: "entry", NodeID: server, Protocol: "http", Listen: "0.0.0.0", Port: 20000, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	src, err := (&importer.Store{Database: s.Database, Vault: s.ConfigurationVault}).Save(ctx, "", importer.Input{Name: "source", Content: "http://user:pass@example.com:8080#A", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, ServerIDs: []string{server}, InboundIDs: []string{inbound.ID}, Imports: []node.GroupImport{{SourceID: src, ProxyKey: "A|http"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Current bulk UI sends only nodeIds, preserving existing imports and
	// explicit server membership while expanding the server's eligible inbounds.
	group, err = nodes.SaveGroup(ctx, group.ID, node.GroupInput{Name: group.Name, Purpose: group.Purpose, Enabled: true, SubscriptionVisible: true, NodeIDs: []string{server}})
	if err != nil || len(group.Imports) != 1 || len(group.ServerIDs) != 1 || len(group.InboundIDs) != 1 {
		t.Fatal("legacy bulk save lost entries", group, err)
	}
	if _, err = s.Database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,20000,20100)", server); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, directory, "group_entries_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		database, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		if err = validateNodeGroupEntries(ctx, database); err != nil {
			return err
		}
		inventory, err := ReadInventory(ctx, database)
		if err != nil {
			return err
		}
		if inventory.Totals.NodeGroupServers != 1 || inventory.Totals.NodeGroupInbounds != 1 || inventory.Totals.NodeGroupImports != 1 {
			t.Fatal(inventory.Totals)
		}
		var n int
		if err = database.QueryRow("SELECT count(*) FROM node_meta WHERE port_range_start=20000 AND port_range_end=20100").Scan(&n); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Explicit membership can stand alone; the archive validator checks both
	// directions of the union, and refuses a missing membership table.
	if _, err = s.Database.Exec("DELETE FROM node_group_inbounds"); err != nil {
		t.Fatal(err)
	}
	if err = validateNodeGroupEntries(ctx, s.Database); err != nil {
		t.Fatal("explicit-only membership rejected", err)
	}
	if _, err = s.Database.Exec("DELETE FROM node_group_members"); err != nil {
		t.Fatal(err)
	}
	if err = validateNodeGroupEntries(ctx, s.Database); err == nil {
		t.Fatal("broken derived membership accepted")
	}
}
