package rules

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRemoveImportedEndpointProfilesAndSnapshot(t *testing.T) {
	s, active := profileFixture(t)
	ctx := context.Background()
	template := Default()
	target := "@endpoint:import:source:id:target"
	other := "@endpoint:import:source:id:other"
	template.Groups[0].Members = []string{target, other}
	template.Groups[0].ExcludeEndpoints = []string{target, "@endpoint:import:another:id:target"}
	before, _, err := s.Save(ctx, template, 0, "owner")
	if err != nil {
		t.Fatal(err)
	}
	inactive := createProfile(t, s, "未启用策略")
	input := profileInput(inactive.Profile)
	input.Template = template
	inactive, _, err = s.WriteProfile(ctx, inactive.Profile.ID, input, false, "owner")
	if err != nil {
		t.Fatal(err)
	}
	active, err = s.Profile(ctx, active.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	refs, err := ImportedEndpointUsageTx(ctx, tx, "source", "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs=%+v", refs)
	}
	for _, ref := range refs {
		if !reflect.DeepEqual(ref.Groups, []string{template.Groups[0].Name}) || ref.Active != (ref.ProfileID == active.Profile.ID) {
			t.Fatalf("ref=%+v", ref)
		}
	}
	if err := RemoveImportedEndpointTx(ctx, tx, "source", "target"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, previous := range []Profile{active.Profile, inactive.Profile} {
		after, err := s.Profile(ctx, previous.ID)
		if err != nil {
			t.Fatal(err)
		}
		g := after.Profile.Template.Groups[0]
		if after.Profile.Revision != previous.Revision+1 || !reflect.DeepEqual(g.Members, []string{other}) || !reflect.DeepEqual(g.ExcludeEndpoints, []string{"@endpoint:import:another:id:target"}) {
			t.Fatalf("profile=%+v", after)
		}
	}
	after, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 || !reflect.DeepEqual(after.Template.Groups[0].Members, []string{other}) {
		t.Fatalf("snapshot=%+v", after)
	}
}

func TestRemoveImportedEndpointEmptyGroupRollback(t *testing.T) {
	s, active := profileFixture(t)
	ctx := context.Background()
	template := Default()
	template.Groups[0].Members = []string{"@endpoint:import:source:id:target"}
	template.Groups[0].EmptyPolicy = "reject"
	before, _, err := s.Save(ctx, template, 0, "owner")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = RemoveImportedEndpointTx(ctx, tx, "source", "target")
	var conflict *ImportedEndpointRemovalError
	if !errors.As(err, &conflict) || conflict.ProfileID != active.Profile.ID || !reflect.DeepEqual(conflict.Groups, []string{template.Groups[0].Name}) {
		t.Fatalf("error=%+v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	after, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("snapshot changed: %+v", after)
	}
}

func TestImportedEndpointUsageSnapshotOnly(t *testing.T) {
	s, active := profileFixture(t)
	ctx := context.Background()
	template := Default()
	target := "@endpoint:import:source:id:target"
	template.Groups[0].ExcludeEndpoints = []string{target}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err := s.saveTx(ctx, tx, template, 0, "owner", false); err != nil {
		t.Fatal(err)
	}
	refs, err := ImportedEndpointUsageTx(ctx, tx, "source", "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ProfileID != active.Profile.ID || !refs[0].Active || !reflect.DeepEqual(refs[0].Groups, []string{template.Groups[0].Name}) {
		t.Fatalf("refs=%+v", refs)
	}
	if err := RemoveImportedEndpointTx(ctx, tx, "source", "target"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Template.Groups[0].ExcludeEndpoints) != 0 || after.Revision != 2 {
		t.Fatalf("snapshot=%+v", after)
	}
	profile, err := s.Profile(ctx, active.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Profile.Revision != active.Profile.Revision {
		t.Fatal("unchanged profile revision advanced")
	}
}
