package node

import "context"

func (r *Repository) GroupIDsForNode(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT g.id FROM node_groups g JOIN node_group_members m ON m.group_id=g.id WHERE m.node_id=? AND g.enabled=1 ORDER BY g.id", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
