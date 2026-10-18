package deployment

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

// Existing terminal results and final accounting facts cannot disappear when
// the authenticated newer source replaces a business snapshot. A source may
// add completed facts; it cannot rewrite an already retained original result.
func requireDeploymentRestoreReceiptsTx(ctx context.Context, tx *sql.Tx) error {
	queries := []string{
		`SELECT count(*) FROM deployment_targets WHERE typeof(revision)<>'integer' OR revision<1 OR typeof(node_id)<>'text' OR length(node_id) NOT BETWEEN 1 AND 128 OR typeof(status)<>'text' OR status NOT IN ('pending','success','failed','rollback') OR typeof(result_code)<>'text' OR typeof(updated_at)<>'text' OR length(updated_at)>64`,
		`SELECT count(*) FROM deployment_commands WHERE typeof(id)<>'text' OR length(id) NOT BETWEEN 1 AND 128 OR typeof(revision)<>'integer' OR revision<0 OR typeof(node_id)<>'text' OR length(node_id) NOT BETWEEN 1 AND 128 OR typeof(status)<>'text' OR status NOT IN ('pending','success','failed') OR typeof(result_code)<>'text' OR typeof(operation)<>'text' OR typeof(created_at)<>'text'`,
		`SELECT count(*) FROM deployment_targets s LEFT JOIN restore_source.deployment_targets c ON c.revision=s.revision AND c.node_id=s.node_id WHERE s.status<>'pending' AND (c.revision IS NULL OR c.status IS NOT s.status OR c.result_code IS NOT s.result_code OR c.updated_at IS NOT s.updated_at)`,
		`SELECT count(*) FROM deployment_commands s LEFT JOIN restore_source.deployment_commands c ON c.id=s.id WHERE s.status<>'pending' AND (c.id IS NULL OR c.node_id IS NOT s.node_id OR c.revision IS NOT s.revision OR c.operation IS NOT s.operation OR c.status IS NOT s.status OR c.result_code IS NOT s.result_code OR c.created_at IS NOT s.created_at)`,
	}
	for _, query := range queries {
		if err := db.AssertRecoveryNone(ctx, tx, query); err != nil {
			return err
		}
	}
	return nil
}
