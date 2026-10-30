package importer

import (
	"context"
	"path/filepath"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
)

func TestRestoreKeepsCurrentSourcesAndRevokedGrants(t *testing.T) {
	current, a, b := storeFixture(t)
	ctx := context.Background()
	input := Input{Name: "recover fixture", Content: "socks5://fixture:fixture@example.com:1080#external", Interval: 60, UserIDs: []string{a}}
	key, err := current.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	groupID := grantImport(t, current, a, key, "")
	candidate, err := db.Open(ctx, filepath.Join(t.TempDir(), "candidate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	var seq int
	var name, path string
	if err = current.Database.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if _, err = candidate.Exec("ATTACH DATABASE ? AS restore_source", path); err != nil {
		t.Fatal(err)
	}
	if _, err = candidate.Exec("INSERT INTO users SELECT * FROM restore_source.users"); err != nil {
		t.Fatal(err)
	}
	restored := &Store{Database: candidate, Vault: current.Vault}
	prepare := func() {
		t.Helper()
		tx, e := candidate.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if e = restored.PrepareRestoreTx(ctx, tx); e != nil {
			t.Fatal(e)
		}
		if e = (&node.Repository{}).PrepareGroupRestoreTx(ctx, tx); e != nil {
			t.Fatal(e)
		}
		if e = (&user.Repository{}).PrepareGroupGrantsRestoreTx(ctx, tx); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	prepare()
	input.Content = ""
	input.UserIDs = []string{b}
	if _, err = current.Save(ctx, key, input); err != nil {
		t.Fatal(err)
	}
	if err = (&user.Repository{Database: current.Database, Vault: current.Vault}).Grant(ctx, a, groupID, true); err != nil {
		t.Fatal(err)
	}
	if err = (&user.Repository{Database: current.Database, Vault: current.Vault}).Grant(ctx, b, groupID, false); err != nil {
		t.Fatal(err)
	}
	prepare()
	for _, test := range []struct {
		id   string
		want int
	}{{a, 0}, {b, 1}} {
		p, _, e := restored.Proxies(ctx, source.Viewer{UserID: test.id, AllowedProtocols: []string{"socks5"}})
		if e != nil || len(p) != test.want {
			t.Fatal("restore resurrected a grant or lost encrypted cache")
		}
	}
	if err = current.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	prepare()
	list, err := restored.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatal("restore resurrected deleted source")
	}
}
