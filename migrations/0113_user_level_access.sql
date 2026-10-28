-- v1.1 user-level access (plan section 4.1): every user keeps exactly one
-- device, which becomes the user's hidden access identity. Agents and every
-- traffic, tombstone and quota path key on that device ID unchanged.
--
-- The primary device is the one holding the user's active subscription token,
-- else the oldest active device, else the oldest device. Every other device is
-- removed: its credentials are tombstoned first, so counters the nodes still
-- report for them are charged instead of rejected (traffic_tombstones.go), and
-- the delete cascades to their credentials, WireGuard addresses and tokens.
--
-- Policies scoped to devices are rewritten to the owning users first, while the
-- devices still exist. A policy whose device scope names no existing device
-- matched nobody before; an empty user scope would match everybody, so such a
-- policy is disabled and its remark says why.

UPDATE policies
SET enabled = 0,
    remark = CASE WHEN remark = '' THEN '' ELSE remark || ' ' END || '(v1.1: device scope named no existing device; disabled)',
    scope_json = json_remove(scope_json, '$.device_ids')
WHERE json_type(scope_json, '$.device_ids') = 'array'
  AND json_array_length(scope_json, '$.device_ids') > 0
  AND NOT EXISTS (SELECT 1 FROM json_each(policies.scope_json, '$.device_ids') AS j JOIN devices AS d ON d.id = j.value)
  AND (json_type(scope_json, '$.user_ids') IS NULL OR json_array_length(scope_json, '$.user_ids') = 0);

UPDATE policies
SET scope_json = json_remove(
      json_set(scope_json, '$.user_ids', json((
        SELECT json_group_array(user_id) FROM (
          SELECT value AS user_id FROM json_each(policies.scope_json, '$.user_ids')
          UNION
          SELECT d.user_id FROM json_each(policies.scope_json, '$.device_ids') AS j JOIN devices AS d ON d.id = j.value
          ORDER BY 1
        )
      ))),
      '$.device_ids')
WHERE json_type(scope_json, '$.device_ids') = 'array';

CREATE TEMP TABLE migration_0113_removed_devices AS
SELECT id FROM (
  SELECT d.id,
         ROW_NUMBER() OVER (
           PARTITION BY d.user_id
           ORDER BY EXISTS (SELECT 1 FROM subscription_tokens AS t WHERE t.device_id = d.id AND t.revoked_at IS NULL) DESC,
                    d.status = 'active' DESC,
                    d.created_at,
                    d.id
         ) AS position
  FROM devices AS d
)
WHERE position > 1;

INSERT INTO credential_tombstones(credential_id, user_id, device_id, deleted_at)
SELECT c.id, c.user_id, c.device_id, strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'
FROM credentials AS c
WHERE c.device_id IN (SELECT id FROM migration_0113_removed_devices)
ON CONFLICT(credential_id) DO NOTHING;

DELETE FROM devices WHERE id IN (SELECT id FROM migration_0113_removed_devices);

DROP TABLE migration_0113_removed_devices;

UPDATE users SET max_devices = 1;
