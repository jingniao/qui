package rules

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func profileFixture(t *testing.T) (*Store, ProfileResult) {
	t.Helper()
	s := openStore(t)
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := s.Profiles(context.Background(), 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Profile(context.Background(), list.ActiveProfileID)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func profileInput(p Profile) ProfileInput {
	return ProfileInput{Name: p.Name, Description: p.Description, Template: p.Template, UIMetadata: p.UIMetadata, ExpectedRevision: p.Revision}
}

func createProfile(t *testing.T, s *Store, name string) ProfileResult {
	t.Helper()
	p, _, err := s.WriteProfile(context.Background(), "", ProfileInput{Name: name, Template: Default(), UIMetadata: json.RawMessage(`{"category":"region"}`)}, false, "owner")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileConcurrentEdits(t *testing.T) {
	s, _ := profileFixture(t)
	p := createProfile(t, s, "inactive")
	var wg sync.WaitGroup
	errorsCh := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			input := profileInput(p.Profile)
			input.Name = name
			_, _, err := s.WriteProfile(context.Background(), p.Profile.ID, input, false, name)
			errorsCh <- err
		}(name)
	}
	wg.Wait()
	close(errorsCh)
	success, conflicts := 0, 0
	for err := range errorsCh {
		if err == nil {
			success++
		} else if errors.Is(err, ErrProfileConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
}

func TestProfileConcurrentActivationAndIdenticalContent(t *testing.T) {
	s, initial := profileFixture(t)
	a, b := createProfile(t, s, "A"), createProfile(t, s, "B")
	var wg sync.WaitGroup
	errorsCh := make(chan error, 2)
	for _, p := range []Profile{a.Profile, b.Profile} {
		wg.Add(1)
		go func(p Profile) {
			defer wg.Done()
			_, _, err := s.WriteProfile(context.Background(), p.ID, ProfileInput{Activate: true, ExpectedRevision: p.Revision, ExpectedActiveRevision: &initial.ActiveRevision}, true, "owner")
			errorsCh <- err
		}(p)
	}
	wg.Wait()
	close(errorsCh)
	success, conflicts := 0, 0
	for err := range errorsCh {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	list, _ := s.Profiles(context.Background(), 1, 100)
	next := a.Profile
	if list.ActiveProfileID == next.ID {
		next = b.Profile
	}
	state, changed, err := s.WriteProfile(context.Background(), next.ID, ProfileInput{Activate: true, ExpectedRevision: next.Revision, ExpectedActiveRevision: &list.ActiveRevision}, true, "owner")
	if err != nil || !changed || state.ActiveRevision != list.ActiveRevision+1 {
		t.Fatal(state, changed, err)
	}
	again, changed, err := s.WriteProfile(context.Background(), next.ID, ProfileInput{Activate: true, ExpectedRevision: next.Revision, ExpectedActiveRevision: &state.ActiveRevision}, true, "owner")
	if err != nil || changed || again.ActiveRevision != state.ActiveRevision {
		t.Fatal("re-activating the active profile changed the version", again, changed, err)
	}
}

func TestProfileSwitchedAwayEditAndNoOp(t *testing.T) {
	s, initial := profileFixture(t)
	ctx := context.Background()
	// Even the unsaved default remains a no-op at activeRevision zero.
	_, changed, err := s.Save(ctx, initial.Profile.Template, 0, "owner")
	if err != nil || changed {
		t.Fatal(changed, err)
	}
	initial, _ = s.Profile(ctx, initial.Profile.ID)
	input := profileInput(initial.Profile)
	input.ExpectedActiveRevision = &initial.ActiveRevision
	same, changed, err := s.WriteProfile(ctx, initial.Profile.ID, input, false, "owner")
	if err != nil || changed || same.ActiveRevision != initial.ActiveRevision || same.Profile.Revision != initial.Profile.Revision {
		t.Fatal(same, changed, err)
	}
	other := createProfile(t, s, "other")
	_, _, err = s.WriteProfile(ctx, other.Profile.ID, ProfileInput{Activate: true, ExpectedRevision: other.Profile.Revision, ExpectedActiveRevision: &initial.ActiveRevision}, true, "owner")
	if err != nil {
		t.Fatal(err)
	}
	input.Name = "edited after switch"
	state, _, err := s.WriteProfile(ctx, initial.Profile.ID, input, false, "owner")
	if !errors.Is(err, ErrConflict) || state.ActiveRevision != initial.ActiveRevision+1 {
		t.Fatal(state, err)
	}
	unchanged, _ := s.Profile(ctx, initial.Profile.ID)
	if unchanged.Profile.Name != initial.Profile.Name {
		t.Fatal("partial profile edit")
	}
}

func TestProfileFailedTransactionRollsBack(t *testing.T) {
	s, initial := profileFixture(t)
	ctx := context.Background()
	if _, err := s.Database.Exec(`CREATE TRIGGER fail_pointer BEFORE UPDATE ON system_settings WHEN NEW.key='client_policy_active' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	input := ProfileInput{Name: "atomic", Template: Presets()[1].Template, UIMetadata: json.RawMessage(`{}`), Activate: true, ExpectedActiveRevision: &initial.ActiveRevision}
	if _, _, err := s.WriteProfile(ctx, "", input, false, "owner"); err == nil {
		t.Fatal("injected failure succeeded")
	}
	list, err := s.Profiles(ctx, 1, 100)
	if err != nil || list.Total != 1 || list.ActiveRevision != 0 || list.ActiveProfileID != initial.Profile.ID {
		t.Fatal(list, err)
	}
	stored, _ := s.Read(ctx)
	if stored.Template.Preset != "standard" {
		t.Fatal("snapshot escaped rollback")
	}
	input = profileInput(initial.Profile)
	input.Name, input.Template, input.ExpectedActiveRevision = "partial", Presets()[1].Template, &initial.ActiveRevision
	if _, err := s.Database.Exec(`CREATE TRIGGER fail_snapshot BEFORE INSERT ON system_settings WHEN NEW.key='client_rules' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WriteProfile(ctx, initial.Profile.ID, input, false, "owner"); err == nil {
		t.Fatal("injected failure succeeded")
	}
	current, _ := s.Profile(ctx, initial.Profile.ID)
	if current.Profile.Name != initial.Profile.Name || current.Profile.Revision != initial.Profile.Revision {
		t.Fatal("profile escaped rollback")
	}
}

func TestProfileInitializationLegacySyncAndImport(t *testing.T) {
	s, initial := profileFixture(t)
	ctx := context.Background()
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Profiles(ctx, 1, 100)
	if list.Total != 1 || list.ActiveRevision != 0 || list.ActiveProfileID != initial.Profile.ID {
		t.Fatal(list)
	}
	saved, _, err := s.Save(ctx, Presets()[1].Template, 0, "owner")
	if err != nil {
		t.Fatal(err)
	}
	current, _ := s.Profile(ctx, initial.Profile.ID)
	if current.Profile.Template.Preset != saved.Template.Preset || current.ActiveRevision != saved.Revision {
		t.Fatal("legacy save diverged")
	}
	if _, err := s.Database.Exec("DELETE FROM client_policy_profiles; DELETE FROM system_settings WHERE key='client_policy_active'"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareRestoreTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Profiles(ctx, 1, 100)
	if list.Total != 1 || list.ActiveRevision != saved.Revision {
		t.Fatal(list)
	}
	current, _ = s.Profile(ctx, list.ActiveProfileID)
	if current.Profile.Template.Preset != "minimal" {
		t.Fatal(current)
	}
	key := "legacy:sha256"
	input := ProfileInput{Name: "import", Template: Default(), UIMetadata: json.RawMessage(`{}`), ImportKey: &key}
	first, _, err := s.WriteProfile(ctx, "", input, false, "owner")
	if err != nil {
		t.Fatal(err)
	}
	second, changed, err := s.WriteProfile(ctx, "", input, false, "owner")
	if err != nil || changed || first.Profile.ID != second.Profile.ID {
		t.Fatal(second, changed, err)
	}
	list, _ = s.Profiles(ctx, 1, 100)
	if list.Total != 2 || list.ActiveProfileID == first.Profile.ID {
		t.Fatal(list)
	}
}

func TestProfileLimitAndPagination(t *testing.T) {
	s, initial := profileFixture(t)
	ctx := context.Background()
	for index := 1; index < 50; index++ {
		createProfile(t, s, "profile")
	}
	input := profileInput(initial.Profile)
	if _, _, err := s.WriteProfile(ctx, "", input, false, "owner"); !errors.Is(err, ErrProfileLimit) {
		t.Fatal(err)
	}
	list, err := s.Profiles(ctx, 3, 20)
	if err != nil || list.Total != 50 || len(list.Items) != 10 {
		t.Fatal(list, err)
	}
}
