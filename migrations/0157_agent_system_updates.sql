ALTER TABLE node_update_capabilities ADD COLUMN agent_system_supported INTEGER NOT NULL DEFAULT 0 CHECK(agent_system_supported IN (0,1));
ALTER TABLE node_update_capabilities ADD COLUMN agent_system_error TEXT NOT NULL DEFAULT '';
ALTER TABLE node_update_capabilities ADD COLUMN agent_bootstrap_version TEXT NOT NULL DEFAULT '';
CREATE TABLE agent_system_update_tasks (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id), baseline_revision INTEGER NOT NULL,
 target_version TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('queued','running','success','failed','unconfirmed')),
 generation INTEGER NOT NULL, attempt_id TEXT NOT NULL, created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 phase TEXT NOT NULL DEFAULT '', result_json BLOB, dispatch_error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX agent_system_update_active_node ON agent_system_update_tasks(node_id) WHERE status IN ('queued','running','unconfirmed');
CREATE TABLE agent_system_update_attempts (
 attempt_id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES agent_system_update_tasks(id), generation INTEGER NOT NULL,
 request_json BLOB NOT NULL, progress_json BLOB, result_json BLOB, log_sequence INTEGER NOT NULL DEFAULT 0,
 log_offset INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, dispatch_error_json BLOB, UNIQUE(task_id,generation)
);
CREATE TABLE agent_system_update_logs (
 attempt_id TEXT NOT NULL REFERENCES agent_system_update_attempts(attempt_id), sequence INTEGER NOT NULL,
 offset INTEGER NOT NULL, sha256 TEXT NOT NULL, content BLOB NOT NULL, PRIMARY KEY(attempt_id,sequence), UNIQUE(attempt_id,offset)
);
