package importer

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"proxycontrol/internal/panel/subscription/source"
)

func TestImportStableIDsAndURLKeys(t *testing.T) {
	for _, kind := range []string{"content", "url"} {
		t.Run(kind, func(t *testing.T) {
			s, a, b := storeFixture(t)
			ctx := context.Background()
			const content = "socks5://user:pass@a.example.com:1080#id:region|A\nsocks5://user:pass@b.example.com:1080#B"
			input := Input{Name: "identity", Content: content, Interval: 60}
			if kind == "url" {
				input.Content, input.URL = "", "https://example.com/fixture"
				s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, content) })
			}
			src, _, err := s.Save(ctx, "", input)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "url" {
				if _, err := s.Refresh(ctx, src); err != nil {
					t.Fatal(err)
				}
			}
			var selected string
			if err := s.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=? AND name='id:region|A'", src).Scan(&selected); err != nil {
				t.Fatal(err)
			}
			items, err := s.Items(ctx)
			if err != nil || len(items) != 2 {
				t.Fatalf("items=%+v err=%v", items, err)
			}
			for _, item := range items {
				want := item["name"].(string) + "|socks5"
				if kind == "content" {
					want = "id:" + item["id"].(string)
				}
				if item["proxyKey"] != want {
					t.Errorf("proxyKey=%v want=%s", item["proxyKey"], want)
				}
			}
			// Seed both key forms directly to exercise authorization independently
			// of SaveGroup validation, including malformed URL ID grants.
			for _, grant := range []struct{ group, user, key string }{
				{"stable", a, "id:" + selected},
				{"legacy", a, "id:region|A|socks5"},
				{"other-user", b, "id:" + selected},
			} {
				if _, err := s.Database.Exec("INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES(?,?,'subscription',1,1,'now')", grant.group, grant.group); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Database.Exec("INSERT INTO node_group_imports VALUES(?,?,?)", grant.group, src, grant.key); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Database.Exec("INSERT INTO user_node_group_grants VALUES(?,?)", grant.user, grant.group); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Database.Exec("INSERT INTO node_group_imports VALUES('legacy',?,?)", src, "id:"+selected); err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				user   string
				labels []string
			}{
				{a, []string{"nodeGroup:legacy", "nodeGroup:stable"}},
				{b, []string{"nodeGroup:other-user"}},
			} {
				want := test.labels
				if kind == "url" {
					want = []string{"nodeGroup:legacy"}
					if test.user == b {
						want = nil
					}
				}
				entries, _, err := s.Proxies(ctx, source.Viewer{UserID: test.user, AllowedProtocols: []string{"socks5"}})
				if err != nil {
					t.Fatal(err)
				}
				if want == nil {
					if len(entries) != 0 {
						t.Fatalf("URL ID grant authorized: %+v", entries)
					}
					continue
				}
				if len(entries) != 1 || entries[0].Origin.Ref != selected {
					t.Fatalf("authorization=%+v", entries)
				}
				sort.Strings(entries[0].Origin.Labels)
				if !reflect.DeepEqual(entries[0].Origin.Labels, want) {
					t.Fatalf("labels=%v want=%v", entries[0].Origin.Labels, want)
				}
			}
		})
	}
}

func TestImportLegacySameKeyRowsAndExactIDGrants(t *testing.T) {
	s, legacyUser, idUser := storeFixture(t)
	ctx := context.Background()
	src, _, err := s.Save(ctx, "", Input{Name: "historical duplicates", Content: identityContent, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var originalID string
	if err := s.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&originalID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec(`INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at)
		SELECT 'historical-second',source_id,name,protocol,payload_encrypted,fetched_at FROM imported_proxies WHERE id=?`, originalID); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []struct{ group, user, key string }{
		{"legacy", legacyUser, "Original|socks5"},
		{"stable", idUser, "id:" + originalID},
	} {
		if _, err := s.Database.Exec("INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES(?,?,'subscription',1,1,'now')", grant.group, grant.group); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Database.Exec("INSERT INTO node_group_imports VALUES(?,?,?)", grant.group, src, grant.key); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Database.Exec("INSERT INTO user_node_group_grants VALUES(?,?)", grant.user, grant.group); err != nil {
			t.Fatal(err)
		}
	}
	entries, _, err := s.Proxies(ctx, source.Viewer{UserID: legacyUser, AllowedProtocols: []string{"socks5"}})
	if err != nil || len(entries) != 2 {
		t.Fatalf("legacy same-key rows=%+v, error=%v", entries, err)
	}
	entries, _, err = s.Proxies(ctx, source.Viewer{UserID: idUser, AllowedProtocols: []string{"socks5"}})
	if err != nil || len(entries) != 1 || entries[0].Origin.Ref != originalID {
		t.Fatalf("stable grant expanded to a same-name row: %+v, error=%v", entries, err)
	}
}

func TestImportIdentityReadsRejectUnreadableSourceKind(t *testing.T) {
	s, a, _ := storeFixture(t)
	ctx := context.Background()
	src, _, err := s.Save(ctx, "", Input{Name: "broken kind", Content: identityContent, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('g','g','subscription',1,1,'now')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("INSERT INTO node_group_imports VALUES('g',?,'')", src); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("INSERT INTO user_node_group_grants VALUES(?,'g')", a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Database.Exec("UPDATE subscription_sources SET url_encrypted=x'00' WHERE id=?", src); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Items(ctx); err == nil {
		t.Fatal("Items accepted unreadable source kind")
	}
	if _, _, err := s.Proxies(ctx, source.Viewer{UserID: a}); err == nil {
		t.Fatal("Proxies accepted unreadable source kind")
	}
}
