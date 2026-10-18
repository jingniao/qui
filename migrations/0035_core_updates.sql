CREATE TABLE release_catalogue (
 id TEXT PRIMARY KEY,
 component TEXT NOT NULL CHECK(component IN ('proxy-agent','xray','sing-box')),
 operating_system TEXT NOT NULL CHECK(operating_system IN ('linux','windows')),
 architecture TEXT NOT NULL CHECK(architecture IN ('amd64','arm64')),
 release_sequence INTEGER NOT NULL CHECK(release_sequence>0),
 version TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64),
 signed_json BLOB NOT NULL,
 signing_public_key TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(component,operating_system,architecture,release_sequence)
);
CREATE TABLE node_update_capabilities (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id),
 core_supported INTEGER NOT NULL CHECK(core_supported IN (0,1))
);
CREATE TABLE core_update_tasks (
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
CREATE UNIQUE INDEX core_update_active_node ON core_update_tasks(node_id) WHERE status IN ('queued','running','failed_closed');
CREATE TABLE core_update_attempts (
 command_id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES core_update_tasks(id),
 generation INTEGER NOT NULL CHECK(generation>0),
 operation TEXT NOT NULL CHECK(operation IN ('install','recover')),
 request_json BLOB NOT NULL,
 result_json BLOB,
 created_at TEXT NOT NULL,
 UNIQUE(task_id,generation)
);
