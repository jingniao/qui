package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
	shared "proxycontrol/pkg/contracts/policy"
	"strings"
	"time"
)

var ErrNotFound = errors.New("policy or service set not found")

type Repository struct{ Database *sql.DB }
type Record struct {
	desiredstate.Policy
	Remark    string `json:"remark"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}
type Input struct {
	Name     string                      `json:"name"`
	Priority int                         `json:"priority"`
	Enabled  bool                        `json:"enabled"`
	Scope    desiredstate.Scope          `json:"scope"`
	Match    desiredstate.Match          `json:"match"`
	Action   desiredstate.Action         `json:"action"`
	Logging  *desiredstate.PolicyLogging `json:"logging"`
	Timezone string                      `json:"timezone"`
	Remark   string                      `json:"remark"`
}
type ServiceSet struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	Version     int      `json:"version"`
	Enabled     bool     `json:"enabled"`
	Description string   `json:"description"`
	Domains     []string `json:"domains"`
	Suffixes    []string `json:"suffixes"`
	CIDRs       []string `json:"cidrs"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}
type ServiceInput struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Description string   `json:"description"`
	Domains     []string `json:"domains"`
	Suffixes    []string `json:"suffixes"`
	CIDRs       []string `json:"cidrs"`
}

func timestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
func (i Input) Rule(ruleID string) desiredstate.Policy {
	return desiredstate.Policy{ID: ruleID, Name: i.Name, Priority: i.Priority, Enabled: i.Enabled, Scope: i.Scope, Match: i.Match, Action: i.Action, Logging: i.Logging, Timezone: i.Timezone}
}

const columns = "id,name,priority,enabled,scope_json,match_json,action_json,logging_json,timezone,remark,created_at,updated_at"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Record, error) {
	var record Record
	var scope, match, action, logging string
	err := row.Scan(&record.ID, &record.Name, &record.Priority, &record.Enabled, &scope, &match, &action, &logging, &record.Timezone, &record.Remark, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return record, err
	}
	for _, field := range []struct {
		data   string
		target any
	}{{scope, &record.Scope}, {match, &record.Match}, {action, &record.Action}, {logging, &record.Logging}} {
		if err := json.Unmarshal([]byte(field.data), field.target); err != nil {
			return record, err
		}
	}
	return record, nil
}
func (r *Repository) Get(ctx context.Context, ruleID string) (Record, error) {
	record, err := scan(r.Database.QueryRowContext(ctx, "SELECT "+columns+" FROM policies WHERE id=?", ruleID))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return record, err
}
func (r *Repository) List(ctx context.Context, keyword string, page, size int) ([]Record, int, error) {
	search := "%" + keyword + "%"
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM policies WHERE name LIKE ?", search).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+columns+" FROM policies WHERE name LIKE ? ORDER BY priority,id LIMIT ? OFFSET ?", search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	records := []Record{}
	for rows.Next() {
		record, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, record)
	}
	return records, count, rows.Err()
}
func (r *Repository) Rules(ctx context.Context) ([]desiredstate.Policy, error) {
	records, count, err := r.List(ctx, "", 1, 512)
	if err != nil {
		return nil, err
	}
	if count > 512 {
		return nil, errors.New("policy limit exceeded")
	}
	rules := []desiredstate.Policy{}
	for _, record := range records {
		if record.Enabled {
			rules = append(rules, record.Policy)
		}
	}
	return rules, nil
}
func (r *Repository) Save(ctx context.Context, ruleID string, input Input) (Record, error) {
	create := ruleID == ""
	if create {
		var err error
		ruleID, err = id.New()
		if err != nil {
			return Record{}, err
		}
	} else {
		if _, err := r.Get(ctx, ruleID); err != nil {
			return Record{}, err
		}
	}
	rule := input.Rule(ruleID)
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" || len(input.Remark) > 1024 {
		return Record{}, errors.New("policy name required or remark too long")
	}
	if err := shared.Validate(&rule); err != nil {
		return Record{}, err
	}
	scope, _ := json.Marshal(rule.Scope)
	match, _ := json.Marshal(rule.Match)
	action, _ := json.Marshal(rule.Action)
	logging, _ := json.Marshal(rule.Logging)
	now := timestamp()
	var err error
	if create {
		var count int
		if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM policies").Scan(&count); err != nil {
			return Record{}, err
		}
		if count >= 512 {
			return Record{}, errors.New("policy limit reached")
		}
		_, err = r.Database.ExecContext(ctx, "INSERT INTO policies(id,name,priority,enabled,scope_json,match_json,action_json,logging_json,timezone,remark,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", ruleID, rule.Name, rule.Priority, rule.Enabled, string(scope), string(match), string(action), string(logging), rule.Timezone, input.Remark, now, now)
	} else {
		_, err = r.Database.ExecContext(ctx, "UPDATE policies SET name=?,priority=?,enabled=?,scope_json=?,match_json=?,action_json=?,logging_json=?,timezone=?,remark=?,updated_at=? WHERE id=?", rule.Name, rule.Priority, rule.Enabled, string(scope), string(match), string(action), string(logging), rule.Timezone, input.Remark, now, ruleID)
	}
	if err != nil {
		return Record{}, err
	}
	return r.Get(ctx, ruleID)
}
func (r *Repository) Delete(ctx context.Context, ruleID string) error {
	if _, err := r.Get(ctx, ruleID); err != nil {
		return err
	}
	_, err := r.Database.ExecContext(ctx, "DELETE FROM policies WHERE id=?", ruleID)
	return err
}
func (r *Repository) Service(ctx context.Context, serviceID string) (ServiceSet, error) {
	service := ServiceSet{Domains: []string{}, Suffixes: []string{}, CIDRs: []string{}}
	err := r.Database.QueryRowContext(ctx, "SELECT id,name,builtin,version,enabled,description,created_at,updated_at FROM service_sets WHERE id=?", serviceID).Scan(&service.ID, &service.Name, &service.Builtin, &service.Version, &service.Enabled, &service.Description, &service.CreatedAt, &service.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return service, ErrNotFound
	}
	if err != nil {
		return service, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT kind,value FROM service_set_entries WHERE service_id=? ORDER BY kind,value", serviceID)
	if err != nil {
		return service, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return service, err
		}
		switch kind {
		case "domain":
			service.Domains = append(service.Domains, value)
		case "suffix":
			service.Suffixes = append(service.Suffixes, value)
		case "cidr":
			service.CIDRs = append(service.CIDRs, value)
		}
	}
	return service, rows.Err()
}
func (r *Repository) Services(ctx context.Context, page, size int) ([]ServiceSet, int, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM service_sets").Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM service_sets ORDER BY builtin DESC,name,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	services := []ServiceSet{}
	for _, id := range ids {
		service, err := r.Service(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		services = append(services, service)
	}
	return services, count, nil
}
func (r *Repository) SaveService(ctx context.Context, serviceID string, input ServiceInput) (ServiceSet, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 || len(input.Description) > 1024 {
		return ServiceSet{}, errors.New("invalid service name or description")
	}
	rule := desiredstate.Policy{ID: "service", Name: input.Name, Match: desiredstate.Match{Domains: input.Domains, Suffixes: input.Suffixes, CIDRs: input.CIDRs}, Action: desiredstate.Action{Type: "Direct"}}
	if err := shared.Validate(&rule); err != nil {
		return ServiceSet{}, err
	}
	if len(rule.Match.Domains)+len(rule.Match.Suffixes)+len(rule.Match.CIDRs) == 0 {
		return ServiceSet{}, errors.New("service needs at least one domain or CIDR")
	}
	create := serviceID == ""
	if create {
		var err error
		serviceID, err = id.New()
		if err != nil {
			return ServiceSet{}, err
		}
	} else {
		if _, err := r.Service(ctx, serviceID); err != nil {
			return ServiceSet{}, err
		}
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return ServiceSet{}, err
	}
	defer tx.Rollback()
	now := timestamp()
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO service_sets(id,name,builtin,enabled,description,created_at,updated_at) VALUES(?,?,0,?,?,?,?)", serviceID, input.Name, input.Enabled, input.Description, now, now)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE service_sets SET name=?,enabled=?,description=?,version=version+1,updated_at=? WHERE id=?", input.Name, input.Enabled, input.Description, now, serviceID)
	}
	if err != nil {
		return ServiceSet{}, errors.New("service name already exists")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM service_set_entries WHERE service_id=?", serviceID); err != nil {
		return ServiceSet{}, err
	}
	for _, entries := range []struct {
		kind   string
		values []string
	}{{"domain", rule.Match.Domains}, {"suffix", rule.Match.Suffixes}, {"cidr", rule.Match.CIDRs}} {
		seen := map[string]bool{}
		for _, value := range entries.values {
			if seen[value] {
				continue
			}
			seen[value] = true
			if _, err := tx.ExecContext(ctx, "INSERT INTO service_set_entries(service_id,kind,value) VALUES(?,?,?)", serviceID, entries.kind, value); err != nil {
				return ServiceSet{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return ServiceSet{}, err
	}
	return r.Service(ctx, serviceID)
}
func (r *Repository) DeleteService(ctx context.Context, serviceID string) error {
	service, err := r.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	if service.Builtin {
		return errors.New("builtin service sets cannot be deleted")
	}
	rules, err := r.Rules(ctx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		for _, id := range rule.Match.ServiceIDs {
			if id == serviceID {
				return errors.New("service set is referenced by an active policy")
			}
		}
	}
	_, err = r.Database.ExecContext(ctx, "DELETE FROM service_sets WHERE id=?", serviceID)
	return err
}
