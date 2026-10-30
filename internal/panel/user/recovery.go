package user

import (
	"context"
	"database/sql"
	"proxycontrol/internal/platform/db"
)

// Group definitions/entries have already been restored by their owner module.
// Current grants win, including revocations since the selected snapshot.
func (r *Repository) PrepareGroupGrantsRestoreTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_node_group_grants SELECT a.* FROM restore_source.user_node_group_grants a JOIN users u ON u.id=a.user_id JOIN node_groups g ON g.id=a.group_id`)
	return err
}

func (r *Repository) PrepareRestoreTx(ctx context.Context, tx *sql.Tx) error {
	for _, prefix := range []string{"", "restore_source."} {
		if err := validateStoredTrafficMaterial(ctx, tx, prefix); err != nil {
			return err
		}
	}
	for _, query := range []string{
		"SELECT count(*) FROM users u JOIN restore_source.users c ON c.id=u.id WHERE c.used_traffic<u.used_traffic AND c.traffic_reset_at<=u.traffic_reset_at",
		"SELECT count(*) FROM devices d JOIN restore_source.devices c ON c.id=d.id JOIN users u ON u.id=d.user_id JOIN restore_source.users cu ON cu.id=c.user_id WHERE c.user_id<>d.user_id OR (c.used_traffic<d.used_traffic AND cu.traffic_reset_at<=u.traffic_reset_at)",
		"SELECT count(*) FROM credentials d JOIN restore_source.credentials c ON c.id=d.id WHERE c.user_id<>d.user_id OR c.device_id<>d.device_id OR c.protocol<>d.protocol",
		"SELECT count(*) FROM user_traffic_cursors s LEFT JOIN restore_source.user_traffic_cursors c ON c.node_id=s.node_id AND c.epoch=s.epoch AND c.credential_id=s.credential_id WHERE c.credential_id IS NULL OR c.user_id<>s.user_id OR c.device_id<>s.device_id OR c.upload_bytes<s.upload_bytes OR c.download_bytes<s.download_bytes",
		"SELECT count(*) FROM users u WHERE u.traffic_limit>0 AND NOT EXISTS(SELECT 1 FROM restore_source.users c WHERE c.id=u.id)",
	} {
		if err := db.AssertRecoveryNone(ctx, tx, query); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,username,display_name,status,expires_at,traffic_limit,used_traffic,max_devices,max_connections,protocols_json,remark,created_at,device_limit)
SELECT c.id,c.username,c.display_name,'disabled',c.expires_at,c.traffic_limit,c.used_traffic,c.max_devices,c.max_connections,c.protocols_json,c.remark,c.created_at,c.device_limit FROM restore_source.users c WHERE NOT EXISTS(SELECT 1 FROM users u WHERE u.id=c.id)`); err != nil {
		return err
	}
	for _, query := range []string{
		"UPDATE users SET status='disabled' WHERE NOT EXISTS(SELECT 1 FROM restore_source.users c WHERE c.id=users.id)",
		`UPDATE users SET traffic_reset_cycle=c.traffic_reset_cycle,traffic_reset_anchor=c.traffic_reset_anchor,traffic_reset_at=c.traffic_reset_at FROM restore_source.users c WHERE c.id=users.id`,
		`UPDATE users SET used_traffic=(SELECT c.used_traffic FROM restore_source.users c WHERE c.id=users.id),status=CASE WHEN status='disabled' THEN 'disabled' ELSE (SELECT c.status FROM restore_source.users c WHERE c.id=users.id) END,
device_limit=CASE WHEN device_limit=0 THEN (SELECT c.device_limit FROM restore_source.users c WHERE c.id=users.id) WHEN (SELECT c.device_limit FROM restore_source.users c WHERE c.id=users.id)=0 THEN device_limit ELSE min(device_limit,(SELECT c.device_limit FROM restore_source.users c WHERE c.id=users.id)) END,
traffic_limit=CASE WHEN traffic_limit=0 THEN (SELECT c.traffic_limit FROM restore_source.users c WHERE c.id=users.id) WHEN (SELECT c.traffic_limit FROM restore_source.users c WHERE c.id=users.id)=0 THEN traffic_limit ELSE min(traffic_limit,(SELECT c.traffic_limit FROM restore_source.users c WHERE c.id=users.id)) END,
expires_at=CASE WHEN expires_at='' THEN (SELECT c.expires_at FROM restore_source.users c WHERE c.id=users.id) WHEN (SELECT c.expires_at FROM restore_source.users c WHERE c.id=users.id)='' THEN expires_at ELSE min(expires_at,(SELECT c.expires_at FROM restore_source.users c WHERE c.id=users.id)) END
WHERE EXISTS(SELECT 1 FROM restore_source.users c WHERE c.id=users.id)`,
		"UPDATE devices SET status='disabled' WHERE NOT EXISTS(SELECT 1 FROM restore_source.devices c WHERE c.id=devices.id)",
		`INSERT INTO devices(id,user_id,name,status,created_at,last_online_at,last_node_id,used_traffic)
SELECT c.id,c.user_id,c.name,'disabled',c.created_at,c.last_online_at,c.last_node_id,c.used_traffic FROM restore_source.devices c WHERE NOT EXISTS(SELECT 1 FROM devices d WHERE d.id=c.id)`,
		"UPDATE devices SET used_traffic=(SELECT c.used_traffic FROM restore_source.devices c WHERE c.id=devices.id),status=CASE WHEN status='disabled' THEN 'disabled' ELSE (SELECT c.status FROM restore_source.devices c WHERE c.id=devices.id) END,last_online_at=(SELECT c.last_online_at FROM restore_source.devices c WHERE c.id=devices.id),last_node_id=(SELECT c.last_node_id FROM restore_source.devices c WHERE c.id=devices.id) WHERE EXISTS(SELECT 1 FROM restore_source.devices c WHERE c.id=devices.id)",
		"UPDATE credentials SET status='revoked' WHERE NOT EXISTS(SELECT 1 FROM restore_source.credentials c WHERE c.id=credentials.id)",
		"UPDATE credentials SET encrypted_secret=(SELECT c.encrypted_secret FROM restore_source.credentials c WHERE c.id=credentials.id),username=(SELECT c.username FROM restore_source.credentials c WHERE c.id=credentials.id),status=CASE WHEN status='revoked' THEN 'revoked' ELSE (SELECT c.status FROM restore_source.credentials c WHERE c.id=credentials.id) END WHERE EXISTS(SELECT 1 FROM restore_source.credentials c WHERE c.id=credentials.id)",
		`INSERT INTO credentials(id,user_id,device_id,protocol,username,encrypted_secret,status,created_at)
SELECT c.id,c.user_id,c.device_id,c.protocol,c.username,c.encrypted_secret,CASE WHEN d.status='active' THEN c.status ELSE 'revoked' END,c.created_at
FROM restore_source.credentials c JOIN devices d ON d.id=c.device_id WHERE NOT EXISTS(SELECT 1 FROM credentials s WHERE s.id=c.id)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if err := db.CopyRecoveryTable(ctx, tx, "user_traffic_cursors", "node_id,epoch,credential_id,user_id,device_id,upload_bytes,download_bytes"); err != nil {
		return err
	}
	// A user or device deleted after the backup was taken left a tombstone in the
	// current database only. Keeping it lets the counters the nodes still send
	// for those credentials stay acceptable after the restore instead of turning
	// into unknown identities. The tombstone carries the exact identity
	// credentialTombstoned matches, and the checks above already refuse a
	// credential whose user or device differs between the two databases, so a
	// carried tombstone can only ever cover the identity that was really deleted
	// and never a credential the restored database still has under another owner.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO credential_tombstones(credential_id,user_id,device_id,deleted_at)
SELECT c.credential_id,c.user_id,c.device_id,c.deleted_at FROM restore_source.credential_tombstones c`); err != nil {
		return err
	}
	if err := validateStoredTrafficMaterial(ctx, tx, ""); err != nil {
		return err
	}
	return nil
}
