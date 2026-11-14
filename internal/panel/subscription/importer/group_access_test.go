package importer

import (
	"context"
	"testing"

	"proxycontrol/internal/panel/subscription/source"
)

func TestImportGroupsExactKeyWholeSourceAndLegacyIgnored(t *testing.T) {
	s, a, b := storeFixture(t)
	ctx := context.Background()
	key, _, err := s.Save(ctx, "", Input{Name: "keys", Content: "socks5://user:pass@a.example.com:1080#A\nsocks5://user:pass@b.example.com:1080#B", Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	viewer := source.Viewer{UserID: a, AllowedProtocols: []string{"socks5"}}
	if p, _, err := s.Proxies(ctx, viewer); err != nil || len(p) != 0 {
		t.Fatal("legacy input granted access", p, err)
	}
	group := grantImport(t, s, a, key, "A|socks5")
	check := func(want int) {
		t.Helper()
		p, _, e := s.Proxies(ctx, viewer)
		if e != nil || len(p) != want {
			t.Fatal(p, e)
		}
	}
	check(1)
	// A new batch with the same names does not inherit this source's grants.
	otherKey, _, err := s.Save(ctx, "", Input{Name: "new keys", Content: "socks5://new:secret@changed.example.com:1081#A\nsocks5://user:pass@b.example.com:1080#B", Interval: 60, UserIDs: []string{b}})
	if err != nil {
		t.Fatal(err)
	}
	check(1)
	if p, _, err := s.Proxies(ctx, source.Viewer{UserID: b, AllowedProtocols: []string{"socks5"}}); err != nil || len(p) != 0 {
		t.Fatal("legacy input granted new batch access", p, err)
	}
	if _, err = s.Database.Exec("INSERT INTO node_group_imports VALUES(?,?,'')", group, key); err != nil {
		t.Fatal(err)
	}
	check(2) // exact and whole-source overlap is deduplicated.
	if _, err = s.Database.Exec("INSERT INTO node_group_imports VALUES(?,?,'A|socks5')", group, otherKey); err != nil {
		t.Fatal(err)
	}
	check(3) // the new source contributes only its explicitly selected endpoint.
	if _, err = s.Database.Exec("UPDATE node_groups SET enabled=0 WHERE id=?", group); err != nil {
		t.Fatal(err)
	}
	check(0)
	items, err := s.Items(ctx)
	if err != nil || len(items) != 4 {
		t.Fatal(items, err)
	}
	for _, item := range items {
		if item["proxyKey"] != "id:"+item["id"].(string) {
			t.Fatal(item)
		}
	}
}
