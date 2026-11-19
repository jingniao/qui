package node

import "testing"

func TestGroupChangesNotifyWithoutGrantedUsers(t *testing.T) {
	r, ctx := deletionRepository(t)
	calls := 0
	r.GroupsChanged = func() { calls++ }
	g, err := r.SaveGroup(ctx, "", GroupInput{Name: "new choices", Purpose: "subscription"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("create notifications %d", calls)
	}
	if _, err := r.SaveGroup(ctx, g.ID, GroupInput{Name: "updated choices", Purpose: "subscription"}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("CRUD notifications %d", calls)
	}
	if err := r.DeleteGroup(ctx, g.ID); err == nil {
		t.Fatal("missing group accepted")
	}
	if calls != 3 {
		t.Fatal("failed delete notified")
	}
}
