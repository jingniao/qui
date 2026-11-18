package backup

import (
	"context"
	"database/sql"
)

func validateNodeLatencyTargets(ctx context.Context, database *sql.DB) error {
	var count int
	for _, q := range []string{
		"SELECT count(*) FROM nodes WHERE latency_targets_custom IS NULL OR latency_targets_custom NOT IN (0,1)",
		"SELECT count(*) FROM (SELECT node_id FROM node_latency_targets GROUP BY node_id HAVING count(*)>5)",
		"SELECT count(*) FROM node_latency_targets nt LEFT JOIN nodes n ON n.id=nt.node_id LEFT JOIN probe_targets t ON t.id=nt.target_id WHERE n.id IS NULL OR t.id IS NULL",
		"SELECT count(*) FROM (SELECT node_id,target_id FROM node_latency_targets GROUP BY node_id,target_id HAVING count(*)>1)",
	} {
		if database.QueryRowContext(ctx, q).Scan(&count) != nil || count != 0 {
			return ErrArchive
		}
	}
	return nil
}
