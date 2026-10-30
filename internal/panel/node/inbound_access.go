package node

import "context"

// UserInboundGroups is the shared inbound-granular subscription boundary. The
// server index is intentionally not an authorization source.
func (r *Repository) UserInboundGroups(ctx context.Context, inboundID, userID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT DISTINCT g.id FROM node_groups g
JOIN user_node_group_grants u ON u.group_id=g.id
JOIN node_group_inbounds e ON e.group_id=g.id
JOIN inbounds i ON i.id=e.inbound_id JOIN nodes n ON n.id=i.node_id
WHERE u.user_id=? AND i.id=? AND g.enabled=1 AND g.subscription_visible=1
AND g.purpose IN ('subscription','mixed') AND i.subscription_visible=1 AND i.internal_relay=0
AND n.enabled=1 AND n.status<>'disabled' AND n.maintenance=0 AND n.allow_subscription=1 AND n.deleted_at IS NULL
AND (json_array_length(i.allowed_users_json)=0 OR EXISTS(SELECT 1 FROM json_each(i.allowed_users_json) WHERE value=?))
ORDER BY g.id`, userID, inboundID, userID)
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

// UserHasNode reports whether at least one eligible inbound on this server is
// reachable. Portal server views must not expose servers via unrelated inbounds.
func (r *Repository) UserHasNode(ctx context.Context, nodeID, userID string) (bool, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM inbounds WHERE node_id=?", nodeID)
	if err != nil {
		return false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		groups, err := r.UserInboundGroups(ctx, id, userID)
		if err != nil {
			return false, err
		}
		if len(groups) > 0 {
			return true, nil
		}
	}
	return false, nil
}
