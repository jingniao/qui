package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
	_ "time/tzdata"
)

type Operation struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Operator  string `json:"operator"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Result    string `json:"result"`
	IP        string `json:"ip"`
}

type Repository struct {
	Database *sql.DB
	Reader   *sql.DB
}

// reader picks the read-only pool for list queries when one is configured.
func (r *Repository) reader() *sql.DB {
	if r.Reader != nil {
		return r.Reader
	}
	return r.Database
}

func (r *Repository) ReadSetup(ctx context.Context) (name, timezone string, err error) {
	settings, err := r.ReadBase(ctx)
	return settings.PlatformName, settings.Timezone, err
}
func (r *Repository) Record(ctx context.Context, operator, action, target, result, ip string) error {
	_, err := r.Database.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,?,?)", time.Now().UTC().Format(time.RFC3339Nano), operator, action, target, result, ip)
	return err
}

func (r *Repository) SaveSetup(ctx context.Context, tx *sql.Tx, platformName, timezone string) error {
	name, zone, err := normalizeBase(platformName, timezone)
	if err != nil {
		return err
	}
	platformName, timezone = name, zone
	for key, value := range map[string]string{"platform_name": platformName, "timezone": timezone} {
		data, _ := json.Marshal(value)
		if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?)", key, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) List(ctx context.Context, keyword string, page, size int) ([]Operation, int, error) {
	search := "%" + keyword + "%"
	where := " WHERE operator LIKE ? OR action LIKE ? OR target LIKE ?"
	var count int
	if err := r.reader().QueryRowContext(ctx, "SELECT count(*) FROM admin_operations"+where, search, search, search).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.reader().QueryContext(ctx, "SELECT id,timestamp,operator,action,target,result,ip FROM admin_operations"+where+" ORDER BY id DESC LIMIT ? OFFSET ?", search, search, search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	operations := []Operation{}
	for rows.Next() {
		var operation Operation
		if err := rows.Scan(&operation.ID, &operation.Timestamp, &operation.Operator, &operation.Action, &operation.Target, &operation.Result, &operation.IP); err != nil {
			return nil, 0, err
		}
		operations = append(operations, operation)
	}
	return operations, count, rows.Err()
}
