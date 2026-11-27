CREATE TABLE server_egress_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  name_pattern TEXT NOT NULL DEFAULT '',
  server_ids_json TEXT NOT NULL CHECK(json_valid(server_ids_json) AND json_type(server_ids_json) = 'array')
);

CREATE TABLE server_egress_relays (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id),
  inbound_id TEXT NOT NULL UNIQUE REFERENCES inbounds(id) ON DELETE CASCADE
);

ALTER TABLE deployment_batch_items ADD COLUMN expected_previous_revision INTEGER;

CREATE TRIGGER server_egress_policy_insert_group BEFORE INSERT ON policies
WHEN json_extract(NEW.action_json, '$.type') = 'ServerEgressGroup'
  AND NOT EXISTS (SELECT 1 FROM server_egress_groups WHERE id = json_extract(NEW.action_json, '$.target_id'))
BEGIN
  SELECT RAISE(ABORT, 'server egress group not found');
END;

CREATE TRIGGER server_egress_policy_update_group BEFORE UPDATE OF action_json ON policies
WHEN json_extract(NEW.action_json, '$.type') = 'ServerEgressGroup'
  AND NOT EXISTS (SELECT 1 FROM server_egress_groups WHERE id = json_extract(NEW.action_json, '$.target_id'))
BEGIN
  SELECT RAISE(ABORT, 'server egress group not found');
END;
