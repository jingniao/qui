package node

import (
	"reflect"
	"testing"
)

func TestImportedProxyGroupsExactRemoval(t *testing.T) {
	r, ctx := groupImportRepository(t)
	g, err := r.SaveGroup(ctx, "", GroupInput{Name: "引用分组", Purpose: "subscription", Imports: []GroupImport{{"content", "id:stable-one"}, {"content", "id:stable-two"}}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := ImportedProxyGroupsTx(ctx, tx, "content", "stable-one")
	if err != nil || !reflect.DeepEqual(got, []ImportGroupUsage{{ID: g.ID, Name: g.Name}}) {
		t.Fatalf("usage=%v error=%v", got, err)
	}
	if err := RemoveImportedProxyGroupsTx(ctx, tx, "content", "stable-one"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := r.Group(ctx, g.ID)
	if err != nil || !reflect.DeepEqual(after.Imports, []GroupImport{{"content", "id:stable-two"}}) {
		t.Fatalf("members=%v error=%v", after.Imports, err)
	}
}
