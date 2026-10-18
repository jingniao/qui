package user

import (
	"context"
	"database/sql"
)

type trafficMaterialReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ValidateTrafficMaterial checks preserved originals, allowing legitimately
// deleted historical accounts while rejecting conflicts with surviving parents.
func ValidateTrafficMaterial(ctx context.Context, q trafficMaterialReader) error {
	return validateStoredTrafficMaterial(ctx, q, "")
}

func validateStoredTrafficMaterial(ctx context.Context, q trafficMaterialReader, prefix string) error {
	if prefix != "" && prefix != "restore_source." {
		return ErrTrafficMaterial
	}
	var present, applied int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"sqlite_master WHERE type='table' AND name='user_traffic_cursors'").Scan(&present); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"schema_migrations WHERE version='0015_account_traffic.sql'").Scan(&applied); err != nil {
		return err
	}
	if present == 0 && applied == 0 {
		return nil
	} // Legacy format only, not zero-history evidence.
	if present != 1 || applied != 1 {
		return ErrTrafficMaterial
	}
	checks := []string{
		"SELECT EXISTS(SELECT 1 FROM " + prefix + "users WHERE typeof(used_traffic)!='integer' OR used_traffic<0 OR typeof(traffic_limit)!='integer' OR traffic_limit<0)",
		"SELECT EXISTS(SELECT 1 FROM " + prefix + "devices WHERE typeof(used_traffic)!='integer' OR used_traffic<0)",
	}
	for _, field := range []string{"node_id", "epoch", "credential_id", "user_id", "device_id"} {
		checks = append(checks, "SELECT EXISTS(SELECT 1 FROM "+prefix+"user_traffic_cursors WHERE typeof("+field+")!='text' OR length(CAST("+field+" AS BLOB)) NOT BETWEEN 1 AND 128)")
	}
	checks = append(checks,
		"SELECT EXISTS(SELECT 1 FROM "+prefix+"user_traffic_cursors WHERE typeof(upload_bytes)!='integer' OR upload_bytes<0 OR typeof(download_bytes)!='integer' OR download_bytes<0)",
		"SELECT EXISTS(SELECT 1 FROM "+prefix+"user_traffic_cursors c JOIN "+prefix+"devices d ON d.id=c.device_id WHERE d.user_id<>c.user_id)",
		"SELECT EXISTS(SELECT 1 FROM "+prefix+"user_traffic_cursors c JOIN "+prefix+"credentials k ON k.id=c.credential_id WHERE k.user_id<>c.user_id OR k.device_id<>c.device_id)",
	)
	for _, query := range checks {
		var invalid int
		if err := q.QueryRowContext(ctx, query).Scan(&invalid); err != nil {
			return err
		}
		if invalid != 0 {
			return ErrTrafficMaterial
		}
	}
	return nil
}
