package proxy

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"proxycontrol/internal/platform/db"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Keep the listener allocations of the live revisions that Deployment
// preserves, even when the backup predates those allocations.
func (r *Repository) PrepareServerEgressRestoreTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT e.inbound_id,c.encrypted_state
	FROM restore_source.server_egress_relays e
	JOIN restore_source.node_runtime_state s ON s.node_id=e.node_id
	JOIN restore_source.config_revisions c ON c.id=s.revision`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var active []string
	for rows.Next() {
		var inboundID string
		var sealed []byte
		if err := rows.Scan(&inboundID, &sealed); err != nil {
			return err
		}
		raw, err := r.Vault.Open(sealed)
		if err != nil {
			return err
		}
		state, err := desiredstate.Decode(raw)
		clear(raw)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(state.Inbounds, func(in desiredstate.Inbound) bool {
			return in.ID == inboundID && in.InternalRelay && in.Enabled
		}) {
			active = append(active, inboundID)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, inboundID := range active {
		if err := r.restoreServerEgressListener(ctx, tx, inboundID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) restoreServerEgressListener(ctx context.Context, tx *sql.Tx, inboundID string) error {
	var conflicts int
	if err := tx.QueryRowContext(ctx, `SELECT count(*)
		FROM restore_source.server_egress_relays live
		JOIN restore_source.inbounds i ON i.id=live.inbound_id
		WHERE live.inbound_id=? AND (EXISTS (SELECT 1 FROM server_egress_relays saved
			WHERE saved.node_id=live.node_id AND saved.inbound_id<>live.inbound_id)
		OR EXISTS (SELECT 1 FROM inbounds saved
			WHERE saved.node_id=i.node_id AND saved.port=i.port AND saved.id<>i.id)
		OR EXISTS (SELECT 1 FROM port_claims saved
			WHERE saved.node_id=i.node_id AND saved.port=i.port))`, inboundID).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts != 0 {
		return fmt.Errorf("保留运行中的服务器出站监听时发现备份端口或监听冲突：%w", db.ErrRecoveryConflict)
	}
	for _, query := range []string{
		`INSERT INTO relay_credentials(id,protocol,username,encrypted_secret,created_at)
		SELECT c.id,c.protocol,c.username,c.encrypted_secret,c.created_at FROM restore_source.relay_credentials c
		JOIN restore_source.inbounds i ON i.relay_credential_id=c.id
		JOIN restore_source.server_egress_relays e ON e.inbound_id=i.id
		WHERE i.id=? AND NOT EXISTS (SELECT 1 FROM relay_credentials saved WHERE saved.id=c.id)`,
		`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,
			allowed_users_json,encrypted_settings,relay_credential_id,created_at,core,tls_source,certificate_id,egress_source_ip)
		SELECT i.id,i.name,i.node_id,i.protocol,i.listen,i.port,i.enabled,i.subscription_visible,i.internal_relay,i.default_outbound,
			i.allowed_users_json,i.encrypted_settings,i.relay_credential_id,i.created_at,i.core,i.tls_source,i.certificate_id,i.egress_source_ip
		FROM restore_source.inbounds i JOIN restore_source.server_egress_relays e ON e.inbound_id=i.id
		WHERE i.id=? ON CONFLICT(id) DO UPDATE SET listen=excluded.listen`,
		`INSERT INTO server_egress_relays(node_id,inbound_id)
		SELECT e.node_id,e.inbound_id FROM restore_source.server_egress_relays e
		WHERE e.inbound_id=? AND NOT EXISTS (SELECT 1 FROM server_egress_relays saved WHERE saved.node_id=e.node_id)`,
	} {
		if _, err := tx.ExecContext(ctx, query, inboundID); err != nil {
			return err
		}
	}
	return nil
}
