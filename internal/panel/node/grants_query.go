package node

import "context"

// SubscriptionGroups lists the subscription groups whose users may hold a
// credential on this node: groups with a member on it, and groups granting a
// tunnel rule whose target inbound runs here (the rule's clients authenticate
// at that inbound).
func (r *Repository) SubscriptionGroups(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT g.id FROM node_groups g WHERE g.enabled=1 AND g.subscription_visible=1 AND g.purpose IN ('subscription','mixed') AND (
EXISTS(SELECT 1 FROM node_group_members m WHERE m.group_id=g.id AND m.node_id=?) OR
EXISTS(SELECT 1 FROM node_group_relays gr JOIN tunnel_rules t ON t.id=gr.rule_id JOIN inbounds i ON i.id=t.target_inbound_id WHERE gr.group_id=g.id AND i.node_id=?))
ORDER BY g.id`, nodeID, nodeID)
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

// relayTargetNodes lists the servers running the target inbounds of a group's
// tunnel rule members.
func (r *Repository) relayTargetNodes(ctx context.Context, groupID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT DISTINCT i.node_id FROM node_group_relays gr JOIN tunnel_rules t ON t.id=gr.rule_id
JOIN inbounds i ON i.id=t.target_inbound_id WHERE gr.group_id=? ORDER BY i.node_id`, groupID)
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

func (r *Repository) SetPublicIP(ctx context.Context, id, publicIP string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE nodes SET public_ip=? WHERE id=?", publicIP, id)
	return err
}
