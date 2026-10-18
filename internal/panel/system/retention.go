package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	contract "proxycontrol/pkg/contracts/access"
	"time"
)

var ErrRetentionConflict = errors.New("retention settings changed; refresh before saving")
var ErrRetentionInvalid = errors.New("invalid retention settings")

type RetentionSettings struct {
	Policy    contract.Retention `json:"policy"`
	Revision  int64              `json:"revision"`
	UpdatedAt string             `json:"updatedAt"`
	UpdatedBy string             `json:"updatedBy"`
}
type RetentionInput struct {
	Policy           contract.Retention `json:"policy"`
	ExpectedRevision int64              `json:"expectedRevision"`
}

func readRetention(ctx context.Context, tx *sql.Tx) (RetentionSettings, error) {
	settings := RetentionSettings{Policy: contract.DefaultRetention}
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='access_retention'").Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, ErrRetentionInvalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF || settings.Revision < 1 || settings.Policy.Validate() != nil {
		return settings, ErrRetentionInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, settings.UpdatedAt); err != nil {
		return settings, ErrRetentionInvalid
	}
	return settings, nil
}
func (r *Repository) AccessRetentionInTx(ctx context.Context, tx *sql.Tx) (contract.Retention, error) {
	settings, err := readRetention(ctx, tx)
	return settings.Policy, err
}
func (r *Repository) ReadRetention(ctx context.Context) (RetentionSettings, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RetentionSettings{}, err
	}
	defer tx.Rollback()
	settings, err := readRetention(ctx, tx)
	if err != nil {
		return settings, err
	}
	return settings, tx.Commit()
}
func (r *Repository) SaveRetention(ctx context.Context, input RetentionInput, actor, ip string) (RetentionSettings, error) {
	if input.Policy.Validate() != nil || input.ExpectedRevision < 0 {
		return RetentionSettings{}, ErrRetentionInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return RetentionSettings{}, err
	}
	defer tx.Rollback()
	current, err := readRetention(ctx, tx)
	if err != nil {
		return current, err
	}
	if input.Policy == current.Policy {
		return current, nil
	}
	if current.Revision != input.ExpectedRevision || current.Revision == math.MaxInt64 {
		return current, ErrRetentionConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := RetentionSettings{Policy: input.Policy, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('access_retention',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data)); err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改访问记录保留策略','access_retention','成功',?)", stamp, actor, ip); err != nil {
		return next, err
	}
	return next, tx.Commit()
}
