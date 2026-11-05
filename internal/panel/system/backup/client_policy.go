package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"proxycontrol/internal/panel/subscription/rules"
)

func validateClientPolicies(ctx context.Context, database *sql.DB) error {
	var applied, present int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0132_client_policy_profiles.sql'").Scan(&applied) != nil ||
		database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='client_policy_profiles'").Scan(&present) != nil || applied != present {
		return ErrArchive
	}
	store := &rules.Store{Database: database}
	stored, err := store.Read(ctx)
	if err != nil {
		return ErrArchive
	}
	var pointer string
	err = database.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='client_policy_active'").Scan(&pointer)
	if applied == 0 {
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrArchive
		}
		return nil
	}
	var active struct {
		ProfileID string `json:"profileId"`
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrArchive
	}
	if err == nil && rules.Decode([]byte(pointer), &active) != nil {
		return ErrArchive
	}
	rows, err := database.QueryContext(ctx, "SELECT id,name,description,template_json,ui_metadata_json,revision,import_key,created_at,created_by,updated_at,updated_by FROM client_policy_profiles")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	count, found := 0, false
	for rows.Next() {
		var key, template, metadata, created, creator, updated, updater string
		var input rules.ProfileInput
		var revision int64
		if rows.Scan(&key, &input.Name, &input.Description, &template, &metadata, &revision, &input.ImportKey, &created, &creator, &updated, &updater) != nil {
			return ErrArchive
		}
		input.UIMetadata = json.RawMessage(metadata)
		if rules.Decode([]byte(template), &input.Template) != nil || rules.ValidateProfile(input) != nil || rules.Validate(ctx, input.Template, nil) != nil || key == "" || revision < 1 || creator == "" || updater == "" {
			return ErrArchive
		}
		if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
			return ErrArchive
		}
		if _, err := time.Parse(time.RFC3339Nano, updated); err != nil {
			return ErrArchive
		}
		if key == active.ProfileID {
			if same, err := rules.Equal(input.Template, stored.Template); err != nil || !same {
				return ErrArchive
			}
			found = true
		}
		count++
	}
	if rows.Err() != nil || count > 50 || count > 0 && !found || count == 0 && pointer != "" {
		return ErrArchive
	}
	return nil
}
