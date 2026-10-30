package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/platform/db"
	"testing"
)

func TestRecoveryKeepsCurrentGroupEntriesAndPortRange(t *testing.T) {
	ctx := context.Background()
	var sourceID, groupID string
	f := listeningFixture(t, func(f *fixture) {
		var err error
		sourceID, err = (&importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}).Save(ctx, "", importer.Input{Name: "group import", Content: "http://user:pass@a.example.com:8080#A\nhttp://user:pass@b.example.com:8080#B", Interval: 60})
		if err != nil {
			t.Fatal(err)
		}
		g, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "current entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, Imports: []node.GroupImport{{SourceID: sourceID}}})
		if err != nil {
			t.Fatal(err)
		}
		groupID = g.ID
		if err = f.users.Grant(ctx, f.user.ID, g.ID, false); err != nil {
			t.Fatal(err)
		}
		if _, err = f.source.Database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,20000,20010)", f.node.ID); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.nodes.SaveGroup(ctx, groupID, node.GroupInput{Name: "current entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, Imports: []node.GroupImport{{SourceID: sourceID, ProxyKey: "B|http"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Database.Exec("UPDATE node_meta SET port_range_start=21000,port_range_end=21010 WHERE node_id=?", f.node.ID); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	p, _, err := (&importer.Store{Database: database, Vault: f.source.ConfigurationVault}).Proxies(ctx, source.Viewer{UserID: f.user.ID, AllowedProtocols: []string{"http"}})
	if err != nil || len(p) != 1 || p[0].Name != "B" {
		t.Fatal("backup restored whole-source access", p, err)
	}
	g, err := (&node.Repository{Database: database}).Group(ctx, groupID)
	if err != nil || len(g.Imports) != 1 || g.Imports[0].ProxyKey != "B|http" || len(g.GrantedUserIDs) != 1 {
		t.Fatal(g, err)
	}
	assertInt(t, database, "SELECT port_range_start FROM node_meta WHERE node_id=?", 21000, f.node.ID)
}
