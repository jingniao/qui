package node

import "context"

func (r *Repository) SubscriptionGroups(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT g.id FROM node_groups g JOIN node_group_members m ON m.group_id=g.id WHERE m.node_id=? AND g.enabled=1 AND g.subscription_visible=1 AND g.purpose IN ('subscription','mixed') ORDER BY g.id", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		groups = append(groups, id)
	}
	return groups, rows.Err()
}

func (r *Repository) SetPublicIP(ctx context.Context, id, publicIP string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE nodes SET public_ip=? WHERE id=?", publicIP, id)
	return err
}
