ALTER TABLE node_update_capabilities ADD COLUMN agent_supported INTEGER NOT NULL DEFAULT 0 CHECK(agent_supported IN (0,1));
CREATE TABLE agent_update_tasks (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 release_id TEXT NOT NULL REFERENCES release_catalogue(id),
 baseline_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('queued','running','success','rolled_back','rejected','failed_closed')),
 generation INTEGER NOT NULL CHECK(generation>0),
 command_id TEXT NOT NULL UNIQUE,
 original_command_id TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 result_json BLOB
);
CREATE UNIQUE INDEX agent_update_active_node ON agent_update_tasks(node_id) WHERE status IN ('queued','running','failed_closed');
CREATE TABLE agent_update_attempts (
 command_id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES agent_update_tasks(id),
 generation INTEGER NOT NULL CHECK(generation>0),
 operation TEXT NOT NULL CHECK(operation IN ('install','recover')),
 request_json BLOB NOT NULL,
 result_json BLOB,
 created_at TEXT NOT NULL,
 UNIQUE(task_id,generation)
);

