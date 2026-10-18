package system

import (
	"context"
	"database/sql"
	"time"
)

func (r *Repository) RecordNodeCertificate(ctx context.Context, tx *sql.Tx, nodeID, action string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,'node-agent',?,?,'成功','mtls')", time.Now().UTC().Format(time.RFC3339Nano), action, nodeID)
	return err
}
