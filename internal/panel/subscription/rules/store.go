package rules

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
)

// settingsKey is the system_settings row the template lives in. The row holds
// only this template, so it needs no migration and is part of every backup.
const settingsKey = "client_rules"

// ErrConflict reports a save built on a revision another Owner already
// replaced.
var ErrConflict = errors.New("client rule template was changed elsewhere")

// ErrStored reports a stored row that is not a valid template.
var ErrStored = errors.New("stored client rule template is invalid")

// Stored is the template with its optimistic-concurrency revision. Revision 0
// means the Owner never saved one and the default preset is in force.
type Stored struct {
	Template  Template `json:"template"`
	Revision  int64    `json:"revision"`
	UpdatedAt string   `json:"updatedAt"`
	UpdatedBy string   `json:"updatedBy"`
}

// Store reads and writes the template row. It touches no other row.
type Store struct {
	Database *sql.DB
	// Lookup checks references when the Owner saves; a stored template is only
	// checked structurally on read, so a service rule deleted later degrades to
	// a rendering warning instead of breaking every subscription.
	Lookup Lookup
}

// normalize gives every list a non-nil value, so the JSON the page receives and
// the comparison of two templates never depend on null versus [].
func normalize(template Template) Template {
	if template.Groups == nil {
		template.Groups = []Group{}
	}
	for index := range template.Groups {
		if template.Groups[index].Members == nil {
			template.Groups[index].Members = []string{}
		}
	}
	if template.Providers == nil {
		template.Providers = []Provider{}
	}
	if template.Rules == nil {
		template.Rules = []Rule{}
	}
	if template.DNS.Nameservers == nil {
		template.DNS.Nameservers = []string{}
	}
	if template.DNS.Fallback == nil {
		template.DNS.Fallback = []string{}
	}
	return template
}

// Decode reads JSON strictly: unknown fields are refused, so what the server
// keeps always matches the typed model.
func Decode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing data after the template")
	}
	return nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func read(ctx context.Context, query rowQuerier) (Stored, error) {
	var data string
	err := query.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", settingsKey).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Stored{Template: Default()}, nil
	}
	if err != nil {
		return Stored{}, err
	}
	var stored Stored
	if err := Decode([]byte(data), &stored); err != nil || stored.Revision < 1 || stored.UpdatedBy == "" {
		return Stored{}, ErrStored
	}
	if _, err := time.Parse(time.RFC3339Nano, stored.UpdatedAt); err != nil {
		return Stored{}, ErrStored
	}
	stored.Template = normalize(stored.Template)
	if Validate(ctx, stored.Template, nil) != nil {
		return Stored{}, ErrStored
	}
	return stored, nil
}

// Read returns the template in force.
func (s *Store) Read(ctx context.Context) (Stored, error) {
	return read(ctx, s.Database)
}

// Template returns only the template in force; the subscription renderer uses
// it.
func (s *Store) Template(ctx context.Context) (Template, error) {
	stored, err := s.Read(ctx)
	return stored.Template, err
}

// Save validates the template and stores it as the next revision. An unchanged
// template is not a new revision. expected must be the revision the caller read.
func (s *Store) Save(ctx context.Context, template Template, expected int64, actor string) (Stored, bool, error) {
	template = normalize(template)
	if err := Validate(ctx, template, s.Lookup); err != nil {
		return Stored{}, false, err
	}
	if actor == "" || expected < 0 {
		return Stored{}, false, errors.New("invalid save request")
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return Stored{}, false, err
	}
	defer tx.Rollback()
	current, err := read(ctx, tx)
	if err != nil {
		return Stored{}, false, err
	}
	if current.Revision != expected || current.Revision == math.MaxInt64 {
		return current, false, ErrConflict
	}
	same, err := equal(current.Template, template)
	if err != nil {
		return Stored{}, false, err
	}
	if same && current.Revision > 0 {
		return current, false, nil
	}
	next := Stored{Template: template, Revision: current.Revision + 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return Stored{}, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", settingsKey, string(data)); err != nil {
		return Stored{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Stored{}, false, err
	}
	return next, true, nil
}

func equal(left, right Template) (bool, error) {
	a, err := json.Marshal(normalize(left))
	if err != nil {
		return false, err
	}
	b, err := json.Marshal(normalize(right))
	if err != nil {
		return false, err
	}
	return bytes.Equal(a, b), nil
}
