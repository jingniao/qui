package node

import (
	"context"
	"database/sql"
	"fmt"
)

// ReconcileImportIdentitiesTx freezes legacy content membership in the caller's transaction.
func ReconcileImportIdentitiesTx(ctx context.Context, tx *sql.Tx, resolve func(sourceID, oldKey string) ([]string, bool)) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT group_id,source_id,proxy_key FROM node_group_imports ORDER BY group_id,source_id,proxy_key")
	if err != nil {
		return nil, err
	}
	type member struct {
		group string
		GroupImport
	}
	var members []member
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.group, &m.SourceID, &m.ProxyKey); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	report := []string{}
	for _, m := range members {
		ids, content := resolve(m.SourceID, m.ProxyKey)
		if !content {
			continue
		}
		if len(ids) == 0 && m.ProxyKey != "" {
			report = append(report, fmt.Sprintf("node_group_imports group=%s source=%s key=%s: unresolved member retained", m.group, m.SourceID, m.ProxyKey))
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM node_group_imports WHERE group_id=? AND source_id=? AND proxy_key=?", m.group, m.SourceID, m.ProxyKey); err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES(?,?,?) ON CONFLICT(group_id,source_id,proxy_key) DO NOTHING", m.group, m.SourceID, "id:"+id); err != nil {
				return nil, err
			}
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT group_id,count(*) FROM node_group_imports GROUP BY group_id HAVING count(*)>1000 ORDER BY group_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		var group string
		var count int
		if err := rows.Scan(&group, &count); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("node group %s import member limit exceeded: %d > 1000", group, count)
	}
	return report, rows.Err()
}
