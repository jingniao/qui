package rules

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"proxycontrol/internal/platform/id"
)

const activeKey = "client_policy_active"

var (
	ErrProfileConflict = errors.New("client policy was changed elsewhere")
	ErrProfileActive   = errors.New("active client policy cannot be deleted")
	ErrProfileLimit    = errors.New("client policy limit reached")
)

type ProfileSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Revision    int64  `json:"revision"`
	CreatedAt   string `json:"createdAt"`
	CreatedBy   string `json:"createdBy"`
	UpdatedAt   string `json:"updatedAt"`
	UpdatedBy   string `json:"updatedBy"`
}

type Profile struct {
	ProfileSummary
	Template   Template        `json:"template"`
	UIMetadata json.RawMessage `json:"uiMetadata"`
}

type ProfileInput struct {
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	Template               Template        `json:"template"`
	UIMetadata             json.RawMessage `json:"uiMetadata"`
	ImportKey              *string         `json:"importKey,omitempty"`
	Activate               bool            `json:"activate,omitempty"`
	ExpectedRevision       int64           `json:"expectedRevision"`
	ExpectedActiveRevision *int64          `json:"expectedActiveRevision,omitempty"`
}

type ProfileResult struct {
	Profile         Profile `json:"profile"`
	ActiveProfileID string  `json:"activeProfileId"`
	ActiveRevision  int64   `json:"activeRevision"`
}

type ProfileList struct {
	Items           []ProfileSummary `json:"items"`
	Total           int              `json:"total"`
	Page            int              `json:"page"`
	PageSize        int              `json:"pageSize"`
	ActiveProfileID string           `json:"activeProfileId"`
	ActiveRevision  int64            `json:"activeRevision"`
}

// ValidateProfile checks request-only fields; template validation is shared
// with the legacy write boundary.
func ValidateProfile(input ProfileInput) error {
	if strings.TrimSpace(input.Name) == "" || utf8.RuneCountInString(input.Name) > 64 {
		return &FieldError{Field: "name", Message: "名称须为 1–64 个字符"}
	}
	if utf8.RuneCountInString(input.Description) > 500 {
		return &FieldError{Field: "description", Message: "描述不能超过 500 个字符"}
	}
	data := bytes.TrimSpace(input.UIMetadata)
	if len(data) > 64<<10 || len(data) == 0 || data[0] != '{' || !json.Valid(data) {
		return &FieldError{Field: "uiMetadata", Message: "界面元数据须为不超过 64 KiB 的 JSON 对象"}
	}
	if input.ExpectedRevision < 0 || input.ExpectedActiveRevision != nil && *input.ExpectedActiveRevision < 0 {
		return &FieldError{Field: "expectedRevision", Message: "版本不能为负数"}
	}
	if input.ImportKey != nil && (len(*input.ImportKey) == 0 || len(*input.ImportKey) > 256) {
		return &FieldError{Field: "importKey", Message: "导入标识须为 1–256 字节"}
	}
	return nil
}

func activeID(ctx context.Context, q rowQuerier) (string, error) {
	var data string
	if err := q.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", activeKey).Scan(&data); err != nil {
		return "", err
	}
	var pointer struct {
		ProfileID string `json:"profileId"`
	}
	if err := Decode([]byte(data), &pointer); err != nil {
		return "", err
	}
	return pointer.ProfileID, nil
}

func pointTo(ctx context.Context, tx *sql.Tx, profileID string) error {
	data, _ := json.Marshal(struct {
		ProfileID string `json:"profileId"`
	}{profileID})
	_, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", activeKey, string(data))
	return err
}

func profileRead(ctx context.Context, q rowQuerier, profileID string) (Profile, error) {
	var p Profile
	var template, metadata string
	err := q.QueryRowContext(ctx, "SELECT id,name,description,template_json,ui_metadata_json,revision,created_at,created_by,updated_at,updated_by FROM client_policy_profiles WHERE id=?", profileID).Scan(&p.ID, &p.Name, &p.Description, &template, &metadata, &p.Revision, &p.CreatedAt, &p.CreatedBy, &p.UpdatedAt, &p.UpdatedBy)
	if err != nil {
		return p, err
	}
	if err := Decode([]byte(template), &p.Template); err != nil {
		return p, err
	}
	p.Template = normalize(p.Template)
	p.UIMetadata = json.RawMessage(metadata)
	return p, nil
}

func initialize(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM client_policy_profiles").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	stored, err := read(ctx, tx)
	if err != nil {
		return err
	}
	actor, stamp := stored.UpdatedBy, stored.UpdatedAt
	if actor == "" {
		actor = "system"
		stamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	p, err := insertProfile(ctx, tx, ProfileInput{Name: "默认订阅模板", Template: stored.Template, UIMetadata: json.RawMessage(`{}`)}, actor, stamp)
	if err != nil {
		return err
	}
	return pointTo(ctx, tx, p.ID)
}

func (s *Store) Initialize(ctx context.Context) error {
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := initialize(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	return initialize(ctx, tx)
}

func insertProfile(ctx context.Context, tx *sql.Tx, input ProfileInput, actor, stamp string) (Profile, error) {
	key, err := id.New()
	if err != nil {
		return Profile{}, err
	}
	p := Profile{ProfileSummary: ProfileSummary{ID: key, Name: input.Name, Description: input.Description, Revision: 1, CreatedAt: stamp, CreatedBy: actor, UpdatedAt: stamp, UpdatedBy: actor}, Template: normalize(input.Template), UIMetadata: input.UIMetadata}
	data, err := json.Marshal(p.Template)
	if err != nil {
		return p, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO client_policy_profiles(id,name,description,template_json,ui_metadata_json,revision,import_key,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,?,?,1,?,?,?,?,?)", p.ID, p.Name, p.Description, string(data), string(p.UIMetadata), input.ImportKey, stamp, actor, stamp, actor)
	return p, err
}

func updateProfile(ctx context.Context, tx *sql.Tx, current, next Profile, actor string) (Profile, bool, error) {
	same, err := Equal(current.Template, next.Template)
	if err != nil {
		return next, false, err
	}
	if same && current.Name == next.Name && current.Description == next.Description && bytes.Equal(current.UIMetadata, next.UIMetadata) {
		return current, false, nil
	}
	next.Revision = current.Revision + 1
	next.UpdatedAt, next.UpdatedBy = time.Now().UTC().Format(time.RFC3339Nano), actor
	next.Template = normalize(next.Template)
	data, err := json.Marshal(next.Template)
	if err != nil {
		return next, false, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE client_policy_profiles SET name=?,description=?,template_json=?,ui_metadata_json=?,revision=?,updated_at=?,updated_by=? WHERE id=?", next.Name, next.Description, string(data), string(next.UIMetadata), next.Revision, next.UpdatedAt, actor, next.ID)
	return next, true, err
}

func result(ctx context.Context, tx *sql.Tx, p Profile) (ProfileResult, error) {
	active, err := activeID(ctx, tx)
	if err != nil {
		return ProfileResult{}, err
	}
	stored, err := read(ctx, tx)
	return ProfileResult{Profile: p, ActiveProfileID: active, ActiveRevision: stored.Revision}, err
}

func (s *Store) Profile(ctx context.Context, key string) (ProfileResult, error) {
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProfileResult{}, err
	}
	defer tx.Rollback()
	p, err := profileRead(ctx, tx, key)
	if err != nil {
		return ProfileResult{}, err
	}
	return result(ctx, tx, p)
}

func (s *Store) Profiles(ctx context.Context, page, size int) (ProfileList, error) {
	tx, err := s.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProfileList{}, err
	}
	defer tx.Rollback()
	state, err := result(ctx, tx, Profile{})
	if err != nil {
		return ProfileList{}, err
	}
	list := ProfileList{Items: []ProfileSummary{}, Page: page, PageSize: size, ActiveProfileID: state.ActiveProfileID, ActiveRevision: state.ActiveRevision}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM client_policy_profiles").Scan(&list.Total); err != nil {
		return list, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,name,description,revision,created_at,created_by,updated_at,updated_by FROM client_policy_profiles ORDER BY created_at,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ProfileSummary
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Revision, &p.CreatedAt, &p.CreatedBy, &p.UpdatedAt, &p.UpdatedBy); err != nil {
			return list, err
		}
		list.Items = append(list.Items, p)
	}
	return list, rows.Err()
}

// WriteProfile handles create, edit and activation with both CAS checks under
// the writer transaction. An edit that supplied an active version must still
// match it even if another Owner has switched away from this profile.
func (s *Store) WriteProfile(ctx context.Context, key string, input ProfileInput, activateOnly bool, actor string) (ProfileResult, bool, error) {
	if !activateOnly {
		if err := ValidateProfile(input); err != nil {
			return ProfileResult{}, false, err
		}
		if err := Validate(ctx, input.Template, s.Lookup); err != nil {
			return ProfileResult{}, false, err
		}
	} else {
		p, err := s.Profile(ctx, key)
		if err != nil {
			return p, false, err
		}
		if err := Validate(ctx, p.Profile.Template, s.Lookup); err != nil {
			return p, false, err
		}
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return ProfileResult{}, false, err
	}
	defer tx.Rollback()
	state, err := result(ctx, tx, Profile{})
	if err != nil {
		return state, false, err
	}
	if key == "" && input.ImportKey != nil {
		var existing string
		err := tx.QueryRowContext(ctx, "SELECT id FROM client_policy_profiles WHERE import_key=?", *input.ImportKey).Scan(&existing)
		if err == nil {
			state.Profile, err = profileRead(ctx, tx, existing)
			return state, false, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return state, false, err
		}
	}
	if key != "" {
		state.Profile, err = profileRead(ctx, tx, key)
		if err != nil {
			return state, false, err
		}
		if state.Profile.Revision != input.ExpectedRevision {
			return state, false, ErrProfileConflict
		}
	}
	activeWrite := input.Activate || key != "" && key == state.ActiveProfileID
	if activeWrite && input.ExpectedActiveRevision == nil {
		return state, false, &FieldError{Field: "expectedActiveRevision", Message: "请提供订阅模板版本"}
	}
	if !activeWrite && input.ExpectedActiveRevision != nil && *input.ExpectedActiveRevision != state.ActiveRevision {
		return state, false, ErrConflict
	}
	changed := false
	if key == "" {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM client_policy_profiles").Scan(&count); err != nil {
			return state, false, err
		}
		if count >= 50 {
			return state, false, ErrProfileLimit
		}
		state.Profile, err = insertProfile(ctx, tx, input, actor, time.Now().UTC().Format(time.RFC3339Nano))
		changed = true
	} else if !activateOnly {
		next := state.Profile
		next.Name, next.Description, next.Template, next.UIMetadata = input.Name, input.Description, input.Template, input.UIMetadata
		state.Profile, changed, err = updateProfile(ctx, tx, state.Profile, next, actor)
	}
	if err != nil {
		return state, false, err
	}
	if activeWrite {
		// Only a switch to another profile advances the version of an
		// unchanged template; re-activating the active one is a no-op.
		stored, snapshotChanged, err := s.saveTx(ctx, tx, state.Profile.Template, *input.ExpectedActiveRevision, actor, input.Activate && state.Profile.ID != state.ActiveProfileID)
		if err != nil {
			return state, false, err
		}
		state.ActiveRevision = stored.Revision
		changed = changed || snapshotChanged
		if input.Activate {
			if err := pointTo(ctx, tx, state.Profile.ID); err != nil {
				return state, false, err
			}
			state.ActiveProfileID = state.Profile.ID
		}
	}
	if err := tx.Commit(); err != nil {
		return state, false, err
	}
	return state, changed, nil
}

func (s *Store) DeleteProfile(ctx context.Context, key string, expected int64) (ProfileResult, error) {
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return ProfileResult{}, err
	}
	defer tx.Rollback()
	p, err := profileRead(ctx, tx, key)
	if err != nil {
		return ProfileResult{}, err
	}
	state, err := result(ctx, tx, p)
	if err != nil {
		return state, err
	}
	if p.Revision != expected {
		return state, ErrProfileConflict
	}
	if key == state.ActiveProfileID {
		return state, ErrProfileActive
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM client_policy_profiles WHERE id=?", key); err != nil {
		return state, err
	}
	return state, tx.Commit()
}
