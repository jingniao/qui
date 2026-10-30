package node

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx, stamp string) error {
	// Telemetry is intentionally absent from restored snapshots.
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_latency_samples"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_tcp_probes WHERE source='scheduled'"); err != nil {
		return err
	}
	// Queued operations from a snapshot must never execute after restore.
	if _, err := tx.ExecContext(ctx, "UPDATE node_tcp_probes SET status='expired' WHERE status='queued'"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET tcp_probe_supported=0"); err != nil {
		return err
	}
	finishEnrollments, err := prepareEnrollmentReceiptRestore(ctx, tx)
	if err != nil {
		return err
	}
	if err := requireRestoreSequenceMaterialTx(ctx, tx); err != nil {
		return err
	}
	if err := preserveAcceptedMessageMaterialTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_update_capabilities"); err != nil {
		return err
	}
	if err := db.AssertRecoveryNone(ctx, tx, "SELECT count(*) FROM nodes n JOIN restore_source.nodes c ON c.id=n.id WHERE c.last_sequence<n.last_sequence"); err != nil {
		return err
	}
	// Newer nodes absent from the chosen business snapshot remain disabled
	// tombstones so issued allowances and received batch identities survive.
	// Their outbound network settings are carried over like their other fields.
	if _, err := tx.ExecContext(ctx, `INSERT INTO nodes(id,name,status,enabled,hostname,operating_system,agent_version,public_ip,outbound_source_ip,outbound_interface,host_source_ipv4,host_source_ipv6,last_sequence,capabilities_json,created_at,allow_subscription,maintenance,deleted_at)
SELECT c.id,c.name,'disabled',0,c.hostname,c.operating_system,c.agent_version,c.public_ip,c.outbound_source_ip,c.outbound_interface,c.host_source_ipv4,c.host_source_ipv6,c.last_sequence,c.capabilities_json,c.created_at,0,1,c.deleted_at
FROM restore_source.nodes c WHERE NOT EXISTS(SELECT 1 FROM nodes n WHERE n.id=c.id)`); err != nil {
		return err
	}
	// A deleted node stays deleted, disabled and out of every subscription; its
	// other fields still follow the snapshot.
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET deleted_at=coalesce(deleted_at,(SELECT c.deleted_at FROM restore_source.nodes c WHERE c.id=nodes.id)) WHERE EXISTS(SELECT 1 FROM restore_source.nodes c WHERE c.id=nodes.id)`); err != nil {
		return err
	}
	for _, query := range []string{
		"UPDATE nodes SET enabled=0,allow_subscription=0 WHERE NOT EXISTS(SELECT 1 FROM restore_source.nodes c WHERE c.id=nodes.id)",
		"UPDATE nodes SET last_sequence=(SELECT c.last_sequence FROM restore_source.nodes c WHERE c.id=nodes.id),enabled=min(enabled,(SELECT c.enabled FROM restore_source.nodes c WHERE c.id=nodes.id),CASE WHEN deleted_at IS NULL THEN 1 ELSE 0 END),allow_subscription=min(allow_subscription,(SELECT c.allow_subscription FROM restore_source.nodes c WHERE c.id=nodes.id),CASE WHEN deleted_at IS NULL THEN 1 ELSE 0 END),maintenance=max(maintenance,(SELECT c.maintenance FROM restore_source.nodes c WHERE c.id=nodes.id)) WHERE EXISTS(SELECT 1 FROM restore_source.nodes c WHERE c.id=nodes.id)",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE node_identities SET revoked_at=coalesce(revoked_at,?) WHERE NOT EXISTS(SELECT 1 FROM restore_source.node_identities c WHERE c.node_id=node_identities.node_id)", stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at,revoked_at)
SELECT node_id,certificate_pem,fingerprint,expires_at,revoked_at FROM restore_source.node_identities WHERE 1
ON CONFLICT(node_id) DO UPDATE SET certificate_pem=excluded.certificate_pem,fingerprint=excluded.fingerprint,expires_at=excluded.expires_at,
revoked_at=CASE WHEN node_identities.fingerprint=excluded.fingerprint THEN coalesce(node_identities.revoked_at,excluded.revoked_at) ELSE excluded.revoked_at END`); err != nil {
		return err
	}
	for _, query := range []string{
		"DELETE FROM node_enrollment_tokens",
		"UPDATE nodes SET status=CASE WHEN enabled=0 THEN 'disabled' WHEN EXISTS(SELECT 1 FROM node_identities i WHERE i.node_id=nodes.id) THEN 'offline' ELSE 'pending' END,last_heartbeat='',heartbeat_json='{}'",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if err := db.CopyRecoveryTable(ctx, tx, "node_certificate_renewals", "node_id,previous_fingerprint,certificate_pem,fingerprint,expires_at,created_at"); err != nil {
		return err
	}
	var receiptTable int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name='node_accepted_messages'").Scan(&receiptTable); err != nil {
		return err
	}
	if receiptTable == 1 {
		if err := db.CopyRecoveryTable(ctx, tx, "node_accepted_messages", "node_id,sequence,digest"); err != nil {
			return err
		}
	}
	if err := finishEnrollments(); err != nil {
		return err
	}
	// Current metadata stays authoritative during in-process recovery. In
	// particular the daily alert claim cannot rewind to the backup's date.
	if _, err := tx.ExecContext(ctx, `INSERT INTO node_meta(node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,last_alert_day)
SELECT node_id,region,remark,renew_date,renew_cycle,renew_price,renew_currency,renew_notice_days,last_alert_day FROM restore_source.node_meta WHERE 1
ON CONFLICT(node_id) DO UPDATE SET region=excluded.region,remark=excluded.remark,renew_date=excluded.renew_date,renew_cycle=excluded.renew_cycle,renew_price=excluded.renew_price,renew_currency=excluded.renew_currency,renew_notice_days=excluded.renew_notice_days,last_alert_day=max(node_meta.last_alert_day,excluded.last_alert_day)`); err != nil {
		return err
	}
	return nil
}
