package node

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestReconcileImportGroupsExpandsAndPreservesDangling(t *testing.T) {
	r, ctx := groupImportRepository(t)
	g, err := r.SaveGroup(ctx, "", GroupInput{Name: "legacy", Purpose: "subscription"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []GroupImport{{"content", ""}, {"content", "same|socks5"}, {"content", "missing|socks5"}, {"other", ""}, {"url", ""}, {"url", "region:node|socks5"}} {
		insert(t, r, ctx, "INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES(?,?,?)", g.ID, item.SourceID, item.ProxyKey)
	}
	exact, err := r.SaveGroup(ctx, "", GroupInput{Name: "exact", Purpose: "subscription"})
	if err != nil {
		t.Fatal(err)
	}
	insert(t, r, ctx, "INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES(?, 'content', 'same|socks5')", exact.ID)
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	report, err := ReconcileImportIdentitiesTx(ctx, tx, func(source, key string) ([]string, bool) {
		if source == "url" {
			return nil, false
		}
		if source == "other" || key == "missing|socks5" {
			return nil, true
		}
		return []string{"new-one", "new-two"}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || !strings.Contains(report[0], "missing|socks5") || !strings.Contains(report[0], g.ID) {
		t.Fatalf("report=%v", report)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := r.Group(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupImport{{"content", "id:new-one"}, {"content", "id:new-two"}, {"content", "missing|socks5"}, {"url", ""}, {"url", "region:node|socks5"}}
	if !reflect.DeepEqual(got.Imports, want) {
		t.Fatalf("members=%v want=%v", got.Imports, want)
	}
	got, err = r.Group(ctx, exact.ID)
	if err != nil || !reflect.DeepEqual(got.Imports, []GroupImport{{"content", "id:new-one"}, {"content", "id:new-two"}}) {
		t.Fatalf("exact members=%v err=%v", got.Imports, err)
	}
}

func TestReconcileImportGroupsMemberLimit(t *testing.T) {
	for _, count := range []int{1000, 1001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r, ctx := groupImportRepository(t)
			g, err := r.SaveGroup(ctx, "", GroupInput{Name: "legacy", Purpose: "subscription"})
			if err != nil {
				t.Fatal(err)
			}
			insert(t, r, ctx, "INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES(?, 'content', '')", g.ID)
			ids := make([]string, count)
			for i := range ids {
				ids[i] = fmt.Sprintf("new-%04d", i)
			}
			tx, err := r.Database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = ReconcileImportIdentitiesTx(ctx, tx, func(string, string) ([]string, bool) { return ids, true })
			if count == 1001 {
				if err == nil {
					t.Fatal("over-limit expansion accepted")
				}
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
				got, err := r.Group(ctx, g.ID)
				if err != nil || !reflect.DeepEqual(got.Imports, []GroupImport{{"content", ""}}) {
					t.Fatalf("rollback members=%v err=%v", got.Imports, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			got, err := r.Group(ctx, g.ID)
			if err != nil || len(got.Imports) != count {
				t.Fatalf("members=%d err=%v", len(got.Imports), err)
			}
		})
	}
}
