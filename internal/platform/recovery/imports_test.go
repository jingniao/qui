package recovery

import (
	"context"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/testutil/dbtest"
)

func TestDatabaseRecoveryPreservesImportsWithoutResurrectingGrants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var kept, removed string
	f := listeningFixture(t, func(f *fixture) {
		store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		var err error
		input := importer.Input{Name: "kept fixture", Content: "http://fixture:fixture@example.com:8080#external", Interval: 60, UserIDs: []string{f.user.ID}}
		kept, _, err = store.Save(ctx, "", input)
		if err != nil {
			t.Fatal(err)
		}
		input.Name = "deleted fixture"
		removed, _, err = store.Save(ctx, "", input)
		if err != nil {
			t.Fatal(err)
		}
	})
	store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
	if _, _, err := store.Save(ctx, kept, importer.Input{Name: "kept fixture", Interval: 60, UserIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, removed); err != nil {
		t.Fatal(err)
	}
	if newSource, _, err := store.Save(ctx, "", importer.Input{Name: "post-backup fixture", Content: "http://fixture:fixture@example.com:8080#new", Interval: 60, UserIDs: []string{f.user.ID}}); err != nil {
		t.Fatal(err)
	} else {
		var proxyID string
		if err := f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", newSource).Scan(&proxyID); err != nil {
			t.Fatal(err)
		}
		g, e := (&node.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}).SaveGroup(ctx, "", node.GroupInput{Name: "new import", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, Imports: []node.GroupImport{{SourceID: newSource, ProxyKey: "id:" + proxyID}}})
		if e != nil {
			t.Fatal(e)
		}
		if e = (&user.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}).Grant(ctx, f.user.ID, g.ID, false); e != nil {
			t.Fatal(e)
		}
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM subscription_sources", 2)
	assertInt(t, restored, "SELECT count(*) FROM subscription_sources WHERE id=? AND latest_result_json<>'' AND last_success_at<>''", 1, kept)
	assertInt(t, restored, "SELECT count(*) FROM subscription_sources WHERE id=? AND last_status='ok'", 1, kept)
	assertInt(t, restored, "SELECT count(*) FROM subscription_sources WHERE id=?", 0, removed)
	assertInt(t, restored, "SELECT count(*) FROM user_source_grants WHERE source_id=?", 0, kept)
	recovered := &importer.Store{Database: restored, Vault: f.source.ConfigurationVault}
	items, err := recovered.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == kept && (item.LatestResult == nil || item.LatestResult.Outcome != "ok" || item.LastSuccess == "") {
			t.Fatalf("restore lost the recorded result: %+v", item)
		}
	}
	p, _, err := recovered.Proxies(ctx, source.Viewer{UserID: f.user.ID, AllowedProtocols: []string{"http"}})
	if err != nil || len(p) != 1 || p[0].Name != "new" {
		t.Fatal("coordinator lost current encrypted imports or restored revoked access")
	}
}
