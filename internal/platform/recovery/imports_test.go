package recovery

import (
	"context"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/platform/db"
)

func TestDatabaseRecoveryPreservesImportsWithoutResurrectingGrants(t *testing.T) {
	ctx := context.Background()
	var kept, removed string
	f := listeningFixture(t, func(f *fixture) {
		store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		var err error
		input := importer.Input{Name: "kept fixture", Content: "http://fixture:fixture@example.com:8080#external", Interval: 60, UserIDs: []string{f.user.ID}}
		kept, err = store.Save(ctx, "", input)
		if err != nil {
			t.Fatal(err)
		}
		input.Name = "deleted fixture"
		removed, err = store.Save(ctx, "", input)
		if err != nil {
			t.Fatal(err)
		}
	})
	store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
	if _, err := store.Save(ctx, kept, importer.Input{Name: "kept fixture", Interval: 60, UserIDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, removed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ctx, "", importer.Input{Name: "post-backup fixture", Content: "http://fixture:fixture@example.com:8080#new", Interval: 60, UserIDs: []string{f.user.ID}}); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM subscription_sources", 2)
	assertInt(t, restored, "SELECT count(*) FROM subscription_sources WHERE id=?", 0, removed)
	assertInt(t, restored, "SELECT count(*) FROM user_source_grants WHERE source_id=?", 0, kept)
	recovered := &importer.Store{Database: restored, Vault: f.source.ConfigurationVault}
	p, _, err := recovered.Proxies(ctx, source.Viewer{UserID: f.user.ID, AllowedProtocols: []string{"http"}})
	if err != nil || len(p) != 1 || p[0].Name != "new" {
		t.Fatal("coordinator lost current encrypted imports or restored revoked access")
	}
}
