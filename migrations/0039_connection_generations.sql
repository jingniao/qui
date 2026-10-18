ALTER TABLE user_node_connections ADD COLUMN previous_id TEXT NOT NULL DEFAULT '';
ALTER TABLE user_node_connections ADD COLUMN previous_sequence INTEGER NOT NULL DEFAULT 0 CHECK(previous_sequence>=0);
CREATE TABLE user_node_connection_retirements (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 slots INTEGER NOT NULL CHECK(slots BETWEEN 1 AND 10000),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 status TEXT NOT NULL CHECK(status='closed'),
 closing_revision INTEGER NOT NULL REFERENCES config_revisions(id) DEFERRABLE INITIALLY DEFERRED,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 previous_id TEXT NOT NULL DEFAULT '',
 previous_sequence INTEGER NOT NULL DEFAULT 0 CHECK(previous_sequence>=0),
 UNIQUE(user_id,node_id,sequence)
);
INSERT INTO user_node_connection_retirements(id,user_id,node_id,slots,sequence,status,closing_revision,created_at,updated_at,previous_id,previous_sequence)
 SELECT id,user_id,node_id,slots,sequence,status,closing_revision,created_at,updated_at,previous_id,previous_sequence
 FROM user_node_connections WHERE status='closed';
