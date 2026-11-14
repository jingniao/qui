package node

import (
	"context"
	"database/sql"
)

type ImportGroupUsage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ImportedProxyGroupsTx reports only the exact stable member being removed.
func ImportedProxyGroupsTx(ctx context.Context, tx *sql.Tx, sourceID, proxyID string) ([]ImportGroupUsage, error) {
	rows, err := tx.QueryContext(ctx, `SELECT g.id,g.name FROM node_groups g JOIN node_group_imports i ON i.group_id=g.id WHERE i.source_id=? AND i.proxy_key=? ORDER BY g.id`, sourceID, "id:"+proxyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []ImportGroupUsage{}
	for rows.Next() {
		var group ImportGroupUsage
		if err := rows.Scan(&group.ID, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

// RemoveImportedProxyGroupsTx leaves the groups and all other members intact.
func RemoveImportedProxyGroupsTx(ctx context.Context, tx *sql.Tx, sourceID, proxyID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM node_group_imports WHERE source_id=? AND proxy_key=?`, sourceID, "id:"+proxyID)
	return err
}
