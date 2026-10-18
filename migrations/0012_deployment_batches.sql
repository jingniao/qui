CREATE TABLE deployment_batches (
 id TEXT PRIMARY KEY, chain_id TEXT NOT NULL, name TEXT NOT NULL, author TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('draft','deploying','success','failed','rolling_back','rolled_back','rollback_failed')),
 created_at TEXT NOT NULL
);
CREATE TABLE deployment_batch_items (
 batch_id TEXT NOT NULL REFERENCES deployment_batches(id) ON DELETE CASCADE,
 position INTEGER NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 previous_revision INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('waiting','pending','success','failed','rollback_pending','rolled_back','rollback_failed')),
 rollback_command_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(batch_id,position), UNIQUE(batch_id,node_id)
);
CREATE TABLE node_runtime_state (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id), revision INTEGER NOT NULL REFERENCES config_revisions(id)
);
INSERT INTO node_runtime_state(node_id,revision)
SELECT node_id,max(revision) FROM deployment_targets WHERE status='success' GROUP BY node_id;
CREATE TABLE deployment_commands (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id), revision INTEGER NOT NULL,
 operation TEXT NOT NULL CHECK(operation IN ('rollback')),
 status TEXT NOT NULL CHECK(status IN ('pending','success','failed')),
 result_code TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
