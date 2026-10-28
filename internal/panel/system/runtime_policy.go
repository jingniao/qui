package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"proxycontrol/internal/panel/subscription/format"
)

// RuntimePolicy is the Owner's runtime settings. The subscription fields live in
// the same row as the session lifetime, so no migration is needed: a row written
// before v1.1 simply lacks the two new keys and is normalised on read.
type RuntimePolicy struct {
	SessionMinutes          int    `json:"sessionMinutes"`
	SubscriptionFormat      string `json:"subscriptionFormat"`
	SubscriptionUpdateHours int    `json:"subscriptionUpdateHours"`
	SubscriptionUADetection bool   `json:"subscriptionUaDetection"`
}

var ErrRuntimeInvalid = errors.New("session lifetime must be 15 to 1440 minutes, the subscription update interval 1 to 168 hours and the subscription format a registered format")

// Default subscription settings. User-Agent detection is on by default: the
// resolver ships no pattern yet, so it changes nothing until a verified one is
// added (docs/v1.1-phase0-findings.md A.7).
const (
	defaultUpdateHours     = 12
	defaultUADetection     = true
	defaultSubscriptFormat = "uri"
	defaultSessionMinutes  = 120
)

type RuntimeSettings struct {
	Policy    RuntimePolicy `json:"policy"`
	Revision  int64         `json:"revision"`
	UpdatedAt string        `json:"updatedAt"`
	UpdatedBy string        `json:"updatedBy"`
}

// RuntimePolicyInput is the write shape. The v1.1 fields are pointers so that a
// client which predates them (the 1.0.x web bundle, or any script) keeps the
// stored value instead of resetting it; merge applies that rule.
type RuntimePolicyInput struct {
	SessionMinutes          int    `json:"sessionMinutes"`
	SubscriptionFormat      string `json:"subscriptionFormat"`
	SubscriptionUpdateHours *int   `json:"subscriptionUpdateHours"`
	SubscriptionUADetection *bool  `json:"subscriptionUaDetection"`
}

// merge combines a save request with the stored policy.
func (i RuntimePolicyInput) merge(current RuntimePolicy) RuntimePolicy {
	policy := RuntimePolicy{SessionMinutes: i.SessionMinutes, SubscriptionFormat: i.SubscriptionFormat, SubscriptionUpdateHours: current.SubscriptionUpdateHours, SubscriptionUADetection: current.SubscriptionUADetection}
	if i.SubscriptionFormat == "" {
		policy.SubscriptionFormat = current.SubscriptionFormat
	}
	if i.SubscriptionUpdateHours != nil {
		policy.SubscriptionUpdateHours = *i.SubscriptionUpdateHours
	}
	if i.SubscriptionUADetection != nil {
		policy.SubscriptionUADetection = *i.SubscriptionUADetection
	}
	return policy
}

type RuntimeInput struct {
	Policy           RuntimePolicyInput `json:"policy"`
	ExpectedRevision int64              `json:"expectedRevision"`
}

// Validate reports whether the policy is usable. known reports whether a
// subscription format ID is registered; the repository supplies it from the
// format registry, so a new format needs no change here.
func (p RuntimePolicy) Validate(known func(string) bool) error {
	if p.SessionMinutes < 15 || p.SessionMinutes > 1440 {
		return ErrRuntimeInvalid
	}
	if p.SubscriptionUpdateHours < 1 || p.SubscriptionUpdateHours > 168 {
		return ErrRuntimeInvalid
	}
	if known == nil || !known(p.SubscriptionFormat) {
		return ErrRuntimeInvalid
	}
	return nil
}

// storedRuntime is the on-disk shape. The new keys are pointers so that a row
// written before v1.1 keeps its defaults instead of reading as "off" and "0".
type storedRuntime struct {
	Policy struct {
		SessionMinutes          int    `json:"sessionMinutes"`
		SubscriptionFormat      string `json:"subscriptionFormat"`
		SubscriptionUpdateHours *int   `json:"subscriptionUpdateHours"`
		SubscriptionUADetection *bool  `json:"subscriptionUaDetection"`
	} `json:"policy"`
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

func readRuntimeSettings(ctx context.Context, tx *sql.Tx, known func(string) bool) (RuntimeSettings, error) {
	result := RuntimeSettings{Policy: RuntimePolicy{SessionMinutes: defaultSessionMinutes, SubscriptionFormat: defaultSubscriptFormat, SubscriptionUpdateHours: defaultUpdateHours, SubscriptionUADetection: defaultUADetection}}
	var data string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='runtime_policy'").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var stored storedRuntime
	if json.Unmarshal([]byte(data), &stored) != nil || stored.Revision < 1 || stored.UpdatedBy == "" {
		return result, ErrRuntimeInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, stored.UpdatedAt); err != nil {
		return result, ErrRuntimeInvalid
	}
	policy := RuntimePolicy{SessionMinutes: stored.Policy.SessionMinutes, SubscriptionFormat: stored.Policy.SubscriptionFormat, SubscriptionUpdateHours: defaultUpdateHours, SubscriptionUADetection: defaultUADetection}
	if stored.Policy.SubscriptionUpdateHours != nil {
		policy.SubscriptionUpdateHours = *stored.Policy.SubscriptionUpdateHours
	}
	if stored.Policy.SubscriptionUADetection != nil {
		policy.SubscriptionUADetection = *stored.Policy.SubscriptionUADetection
	}
	result = RuntimeSettings{Policy: policy, Revision: stored.Revision, UpdatedAt: stored.UpdatedAt, UpdatedBy: stored.UpdatedBy}
	return result, policy.Validate(known)
}

// formats reports whether a subscription format is registered. Before the server
// wires the format registry the check falls back to the three formats every v1.1
// panel understands.
func (r *Repository) formats(id string) bool {
	if r.SubscriptionFormats != nil {
		return r.SubscriptionFormats(id)
	}
	switch id {
	case "uri", "base64", "sing-box":
		return true
	}
	return false
}

func (r *Repository) ReadRuntimeSettings(ctx context.Context) (RuntimeSettings, error) {
	tx, err := r.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer tx.Rollback()
	value, err := readRuntimeSettings(ctx, tx, r.formats)
	if err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (r *Repository) SaveRuntimeSettings(ctx context.Context, input RuntimeInput, actor, ip string) (RuntimeSettings, bool, error) {
	if input.ExpectedRevision < 0 || actor == "" {
		return RuntimeSettings{}, false, ErrRuntimeInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeSettings{}, false, err
	}
	defer tx.Rollback()
	current, err := readRuntimeSettings(ctx, tx, r.formats)
	if err != nil {
		return current, false, err
	}
	policy := input.Policy.merge(current.Policy)
	if policy.Validate(r.formats) != nil {
		return RuntimeSettings{}, false, ErrRuntimeInvalid
	}
	if policy == current.Policy {
		return current, false, nil
	}
	if current.Revision != input.ExpectedRevision || current.Revision == math.MaxInt64 {
		return current, false, ErrBaseConflict
	}
	next := RuntimeSettings{Policy: policy, Revision: current.Revision + 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedBy: actor}
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

// SubscriptionPolicy reports the settings the subscription renderer needs.
func (r *Repository) SubscriptionPolicy(ctx context.Context) (format.Policy, error) {
	s, err := r.ReadRuntimeSettings(ctx)
	return format.Policy{Default: s.Policy.SubscriptionFormat, DetectUserAgent: s.Policy.SubscriptionUADetection, UpdateHours: s.Policy.SubscriptionUpdateHours}, err
}
