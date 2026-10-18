package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"
)

type RuntimePolicy struct {
	SessionMinutes     int    `json:"sessionMinutes"`
	SubscriptionFormat string `json:"subscriptionFormat"`
}

var ErrRuntimeInvalid = errors.New("session lifetime must be 15 to 1440 minutes and subscription format must be uri, base64 or sing-box")

type RuntimeSettings struct {
	Policy    RuntimePolicy `json:"policy"`
	Revision  int64         `json:"revision"`
	UpdatedAt string        `json:"updatedAt"`
	UpdatedBy string        `json:"updatedBy"`
}
type RuntimeInput struct {
	Policy           RuntimePolicy `json:"policy"`
	ExpectedRevision int64         `json:"expectedRevision"`
}

func (p RuntimePolicy) Validate() error {
	if p.SessionMinutes < 15 || p.SessionMinutes > 1440 {
		return ErrRuntimeInvalid
	}
	switch p.SubscriptionFormat {
	case "uri", "base64", "sing-box":
		return nil
	}
	return ErrRuntimeInvalid
}
func readRuntimeSettings(ctx context.Context, tx *sql.Tx) (RuntimeSettings, error) {
	result := RuntimeSettings{Policy: RuntimePolicy{SessionMinutes: 120, SubscriptionFormat: "uri"}}
	var data string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='runtime_policy'").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if json.Unmarshal([]byte(data), &result) != nil || result.Revision < 1 || result.UpdatedBy == "" {
		return result, ErrRuntimeInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, result.UpdatedAt); err != nil {
		return result, ErrRuntimeInvalid
	}
	return result, result.Policy.Validate()
}
func (r *Repository) ReadRuntimeSettings(ctx context.Context) (RuntimeSettings, error) {
	tx, err := r.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer tx.Rollback()
	value, err := readRuntimeSettings(ctx, tx)
	if err != nil {
		return value, err
	}
	return value, tx.Commit()
}
func (r *Repository) SaveRuntimeSettings(ctx context.Context, input RuntimeInput, actor, ip string) (RuntimeSettings, bool, error) {
	if input.Policy.Validate() != nil || input.ExpectedRevision < 0 || actor == "" {
		return RuntimeSettings{}, false, ErrRuntimeInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeSettings{}, false, err
	}
	defer tx.Rollback()
	current, err := readRuntimeSettings(ctx, tx)
	if err != nil {
		return current, false, err
	}
	if input.Policy == current.Policy {
		return current, false, nil
	}
	if current.Revision != input.ExpectedRevision || current.Revision == math.MaxInt64 {
		return current, false, ErrBaseConflict
	}
	next := RuntimeSettings{Policy: input.Policy, Revision: current.Revision + 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedBy: actor}
	encoded, err := json.Marshal(next)
	if err != nil {
		return next, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('runtime_policy',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(encoded)); err != nil {
		return next, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改会话与订阅设置','runtime_policy','成功',?)", next.UpdatedAt, actor, ip); err != nil {
		return next, false, err
	}
	if err := tx.Commit(); err != nil {
		return next, false, err
	}
	return next, true, nil
}
func (r *Repository) SessionLifetime(ctx context.Context) (time.Duration, error) {
	s, err := r.ReadRuntimeSettings(ctx)
	return time.Duration(s.Policy.SessionMinutes) * time.Minute, err
}
func (r *Repository) DefaultSubscriptionFormat(ctx context.Context) (string, error) {
	s, err := r.ReadRuntimeSettings(ctx)
	return s.Policy.SubscriptionFormat, err
}
