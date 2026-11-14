package rules

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func identityResolver(source, key string) ([]string, bool) {
	if source == "empty" {
		return nil, true
	}
	if source != "content" {
		return nil, false
	}
	switch key {
	case "", "same|socks5":
		return []string{"new-a", "new-b"}, true
	case "id:old-a":
		return []string{"new-a"}, true
	default:
		return nil, true
	}
}

func TestReconcileImportIdentitiesProfilesAndSnapshot(t *testing.T) {
	s, active := profileFixture(t)
	ctx := context.Background()
	template := Default()
	template.Groups[0].Members = []string{"@endpoint:import:content:same|socks5", "@source:content", "@source:url", "@endpoint:import:url:id:name|socks5", "@endpoint:import:content:missing|socks5"}
	template.Groups[0].ExcludeEndpoints = []string{"@endpoint:import:content:id:old-a", "@endpoint:import:content:same|socks5", "@endpoint:import:url:remote|socks5", "@endpoint:import:content:missing|socks5"}
	stored, _, err := s.Save(ctx, template, 0, "owner")
	if err != nil {
		t.Fatal(err)
	}
	inactive := createProfile(t, s, "inactive")
	input := profileInput(inactive.Profile)
	input.Template = template
	inactive, _, err = s.WriteProfile(ctx, inactive.Profile.ID, input, false, "owner")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Profile(ctx, active.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	report, err := ReconcileImportIdentitiesTx(ctx, tx, identityResolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wantMembers := []string{"@endpoint:import:content:id:new-a", "@endpoint:import:content:id:new-b", "@source:url", "@endpoint:import:url:id:name|socks5", "@endpoint:import:content:missing|socks5"}
	wantExcluded := []string{"@endpoint:import:content:id:new-a", "@endpoint:import:content:id:new-b", "@endpoint:import:url:remote|socks5", "@endpoint:import:content:missing|socks5"}
	for _, old := range []Profile{before.Profile, inactive.Profile} {
		got, err := s.Profile(ctx, old.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile.Revision != old.Revision+1 || !reflect.DeepEqual(got.Profile.Template.Groups[0].Members, wantMembers) || !reflect.DeepEqual(got.Profile.Template.Groups[0].ExcludeEndpoints, wantExcluded) {
			t.Fatalf("profile=%+v", got.Profile)
		}
	}
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != stored.Revision+1 || !reflect.DeepEqual(got.Template.Groups[0].Members, wantMembers) || !reflect.DeepEqual(got.Template.Groups[0].ExcludeEndpoints, wantExcluded) {
		t.Fatalf("snapshot=%+v", got)
	}
	if len(report) != 6 || !strings.Contains(strings.Join(report, "\n"), "missing|socks5") {
		t.Fatalf("report=%v", report)
	}
}

func TestReconcileImportIdentitiesEmptySource(t *testing.T) {
	for _, members := range [][]string{{"@source:empty"}, {"@source:empty", TargetReject}} {
		t.Run(strings.Join(members, ","), func(t *testing.T) {
			s, _ := profileFixture(t)
			ctx := context.Background()
			template := Default()
			template.Groups[0].Members = members
			template.Groups[0].EmptyPolicy = "reject"
			if _, _, err := s.Save(ctx, template, 0, "owner"); err != nil {
				t.Fatal(err)
			}
			tx, err := s.Database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			report, err := ReconcileImportIdentitiesTx(ctx, tx, identityResolver)
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			got, err := s.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"@source:empty"}
			if len(members) == 2 {
				want = []string{TargetReject}
			}
			if !reflect.DeepEqual(got.Template.Groups[0].Members, want) || got.Template.Groups[0].EmptyPolicy != "reject" || len(report) < 2 {
				t.Fatalf("got=%+v report=%v", got, report)
			}
		})
	}
}

func TestReconcileImportIdentitiesURLUnchanged(t *testing.T) {
	s, _ := profileFixture(t)
	ctx := context.Background()
	template := Default()
	template.Groups[0].Members = []string{"@source:url", "@endpoint:import:url:name|socks5"}
	before, _, err := s.Save(ctx, template, 0, "owner")
	if err != nil {
		t.Fatal(err)
	}
	var rawBefore string
	if err := s.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='client_rules'").Scan(&rawBefore); err != nil {
		t.Fatal(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	report, err := ReconcileImportIdentitiesTx(ctx, tx, identityResolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var rawAfter string
	if err := s.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='client_rules'").Scan(&rawAfter); err != nil {
		t.Fatal(err)
	}
	after, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rawAfter != rawBefore || after.Revision != before.Revision || len(report) != 0 {
		t.Fatalf("URL snapshot changed; report=%v", report)
	}
}

func TestReconcileImportIdentitiesLargeSource(t *testing.T) {
	s, _ := profileFixture(t)
	ctx := context.Background()
	template := Default()
	template.Groups[0].Members = []string{"@source:content"}
	if _, _, err := s.Save(ctx, template, 0, "owner"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("new-%d", i)
	}
	if _, err := ReconcileImportIdentitiesTx(ctx, tx, func(source, key string) ([]string, bool) {
		return ids, source == "content"
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Template.Groups[0].Members) != 1000 || got.Template.Groups[0].Members[999] != "@endpoint:import:content:id:new-999" {
		t.Fatalf("expanded members=%v", got.Template.Groups[0].Members)
	}
}
