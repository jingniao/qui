-- Preserve every task, attempt, result and log while extending coordination states.
CREATE TEMP TABLE reinstall_agent_system_update_tasks AS SELECT * FROM agent_system_update_tasks;
CREATE TEMP TABLE reinstall_agent_system_update_attempts AS SELECT * FROM agent_system_update_attempts;
CREATE TEMP TABLE reinstall_agent_system_update_logs AS SELECT * FROM agent_system_update_logs;
CREATE TEMP TABLE reinstall_agent_update_tasks AS SELECT * FROM agent_update_tasks;
CREATE TEMP TABLE reinstall_agent_update_attempts AS SELECT * FROM agent_update_attempts;
CREATE TEMP TABLE reinstall_core_update_tasks AS SELECT * FROM core_update_tasks;
DROP TABLE agent_system_update_logs;
DROP TABLE agent_system_update_attempts;
DROP TABLE agent_update_attempts;
DROP TABLE agent_system_update_tasks;
DROP TABLE agent_update_tasks;
DROP TABLE core_update_tasks;
CREATE TABLE agent_system_update_tasks (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id), baseline_revision INTEGER NOT NULL,
 target_version TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('queued','running','success','failed','unconfirmed','superseded')),
 generation INTEGER NOT NULL, attempt_id TEXT NOT NULL, created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 phase TEXT NOT NULL DEFAULT '', result_json BLOB, dispatch_error TEXT NOT NULL DEFAULT ''
);
INSERT INTO agent_system_update_tasks SELECT * FROM reinstall_agent_system_update_tasks;
DROP TABLE reinstall_agent_system_update_tasks;
CREATE TABLE agent_system_update_attempts (
 attempt_id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES agent_system_update_tasks(id), generation INTEGER NOT NULL,
 request_json BLOB NOT NULL, progress_json BLOB, result_json BLOB, log_sequence INTEGER NOT NULL DEFAULT 0,
 log_offset INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, dispatch_error_json BLOB, UNIQUE(task_id,generation)
);
INSERT INTO agent_system_update_attempts SELECT * FROM reinstall_agent_system_update_attempts;
DROP TABLE reinstall_agent_system_update_attempts;
CREATE TABLE agent_system_update_logs (
 attempt_id TEXT NOT NULL REFERENCES agent_system_update_attempts(attempt_id), sequence INTEGER NOT NULL,
 offset INTEGER NOT NULL, sha256 TEXT NOT NULL, content BLOB NOT NULL, PRIMARY KEY(attempt_id,sequence), UNIQUE(attempt_id,offset)
);
INSERT INTO agent_system_update_logs SELECT * FROM reinstall_agent_system_update_logs;
DROP TABLE reinstall_agent_system_update_logs;
CREATE TABLE agent_update_tasks (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 release_id TEXT NOT NULL REFERENCES release_catalogue(id),
 baseline_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('queued','running','success','rolled_back','rejected','failed_closed','superseded')),
 generation INTEGER NOT NULL CHECK(generation>0),
 command_id TEXT NOT NULL UNIQUE,
 original_command_id TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 result_json BLOB
);
INSERT INTO agent_update_tasks SELECT * FROM reinstall_agent_update_tasks;
DROP TABLE reinstall_agent_update_tasks;
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
INSERT INTO agent_update_attempts SELECT * FROM reinstall_agent_update_attempts;
DROP TABLE reinstall_agent_update_attempts;
CREATE TABLE "core_update_tasks" (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id),
  baseline_revision INTEGER NOT NULL REFERENCES config_revisions(id),
  release_id TEXT REFERENCES release_catalogue(id),
  component TEXT NOT NULL CHECK(component IN ('xray','sing-box','gost')),
  version TEXT NOT NULL,
  url TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64),
  sequence INTEGER NOT NULL CHECK(sequence>0),
  status TEXT NOT NULL CHECK(status IN ('queued','running','success','rolled_back','rejected','failed_closed','cleared','superseded')),
  command_id TEXT NOT NULL UNIQUE,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  result_json BLOB,
  adding INTEGER NOT NULL DEFAULT 0 CHECK(adding IN (0,1))
);
INSERT INTO core_update_tasks SELECT * FROM reinstall_core_update_tasks;
DROP TABLE reinstall_core_update_tasks;
CREATE UNIQUE INDEX agent_system_update_active_node ON agent_system_update_tasks(node_id) WHERE status IN ('queued','running','unconfirmed');
CREATE UNIQUE INDEX agent_update_active_node ON agent_update_tasks(node_id) WHERE status IN ('queued','running','failed_closed');
CREATE UNIQUE INDEX core_update_active_component ON core_update_tasks(node_id,component) WHERE status IN ('queued','running');
