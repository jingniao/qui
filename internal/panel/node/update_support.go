package node

import "context"

func (r *Repository) CoreUpdateSupport(ctx context.Context, nodeID string, supported bool) error {
	_, err := r.Database.ExecContext(ctx, "INSERT INTO node_update_capabilities(node_id,core_supported) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET core_supported=excluded.core_supported", nodeID, supported)
	return err
}

func (r *Repository) AgentUpdateSupport(ctx context.Context, nodeID string, supported bool, recovery ...bool) error {
	canRecover := supported && len(recovery) > 0 && recovery[0]
	_, err := r.Database.ExecContext(ctx, "INSERT INTO node_update_capabilities(node_id,core_supported,agent_supported,agent_recovery_supported) VALUES(?,0,?,?) ON CONFLICT(node_id) DO UPDATE SET agent_supported=excluded.agent_supported,agent_recovery_supported=excluded.agent_recovery_supported", nodeID, supported, canRecover)
	return err
}
