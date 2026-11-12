package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/testutil/dbtest"
)

func TestClientPolicyArchiveRoundTrip(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	store := &rules.Store{Database: source.Database}
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	activeRevision := int64(0)
	input := rules.ProfileInput{Name: "归档模板", Description: "description", Template: rules.Presets()[1].Template, UIMetadata: json.RawMessage(`{"opaque":{"value":7}}`), Activate: true, ExpectedActiveRevision: &activeRevision}
	input.Template.Groups[0].IncludeKeywords = []string{"US", "美国"}
	input.Template.Groups[0].ExcludeKeywords = []string{"维护"}
	input.Template.Groups[0].EmptyPolicy = "reject"
	input.Template.Groups[0].Members = []string{"@endpoint:inbound:one", "@endpoint:relay:rule", "@endpoint:import:source:name|socks5"}
	saved, _, err := store.WriteProfile(ctx, "", input, false, "owner")
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "client_policy_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		if err := validateClientPolicies(ctx, database); err != nil {
			return err
		}
		inventory, err := ReadInventory(ctx, database)
		if err != nil {
			return err
		}
		if inventory.Totals.ClientPolicyProfiles != 2 || inventory.Totals.ClientPolicyActive != 1 {
			t.Fatal(inventory.Totals)
		}
		archived, err := (&rules.Store{Database: database}).Profile(ctx, saved.Profile.ID)
		if err != nil {
			return err
		}
		if archived.ActiveRevision != saved.ActiveRevision || archived.ActiveProfileID != saved.Profile.ID || string(archived.Profile.UIMetadata) != string(saved.Profile.UIMetadata) {
			t.Fatal(archived)
		}
		if !reflect.DeepEqual(archived.Profile.Template, saved.Profile.Template) {
			t.Fatal("membership fields changed in archive", archived.Profile.Template)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientPolicyArchiveValidation(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		`UPDATE system_settings SET value_json='{"profileId":"missing"}' WHERE key='client_policy_active'`,
		`DELETE FROM system_settings WHERE key='client_policy_active'`,
		`UPDATE client_policy_profiles SET template_json=replace(template_json,'"mixedPort":7890','"mixedPort":7891')`,
		`UPDATE client_policy_profiles SET ui_metadata_json='[]'`,
		`UPDATE client_policy_profiles SET revision=0`,
		`DROP TABLE client_policy_profiles`,
	} {
		t.Run(query, func(t *testing.T) {
			source, _, _ := sourceFixture(t)
			ctx := context.Background()
			store := &rules.Store{Database: source.Database}
			if err := store.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			if err := validateClientPolicies(ctx, source.Database); err != nil {
				t.Fatal(err)
			}
			if _, err := source.Database.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
			if _, err := source.Database.Exec(query); err != nil {
				t.Fatal(err)
			}
			if validateClientPolicies(ctx, source.Database) == nil {
				t.Fatal("invalid client policy archive accepted")
			}
		})
	}
}

func TestPre0132ClientPolicyArchiveInitialization(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	// 0134 came after the client-policy library, so a pre-0132 archive cannot
	// keep its recovery objects either.
	rewindIdentityRecovery(t, source.Database)
	for _, query := range []string{
		"DROP TABLE client_policy_profiles",
		"DELETE FROM schema_migrations WHERE version='0132_client_policy_profiles.sql'",
		// 0133 came after the client-policy library, so an archive rewound to
		// pre-0132 cannot keep its result columns either.
		"ALTER TABLE subscription_sources DROP COLUMN latest_result_json",
		"ALTER TABLE subscription_sources DROP COLUMN last_success_at",
		"DELETE FROM schema_migrations WHERE version='0133_subscription_source_results.sql'",
	} {
		if _, err := source.Database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateClientPolicies(ctx, source.Database); err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "pre0132_client_policy", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := dbtest.Open(ctx, filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		store := &rules.Store{Database: database}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := store.PrepareRestoreTx(ctx, tx); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if err := store.Initialize(ctx); err != nil {
			return err
		}
		list, err := store.Profiles(ctx, 1, 100)
		if err != nil {
			return err
		}
		if list.Total != 1 || list.ActiveRevision != 0 {
			t.Fatal(list)
		}
		return validateClientPolicies(ctx, database)
	})
	if err != nil {
		t.Fatal(err)
	}
}
