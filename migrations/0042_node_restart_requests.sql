ALTER TABLE nodes ADD COLUMN core_restart_supported INTEGER NOT NULL DEFAULT 0 CHECK(core_restart_supported IN (0,1));
CREATE TABLE node_restart_requests (
 command_id TEXT PRIMARY KEY REFERENCES deployment_commands(id),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 created_at TEXT NOT NULL
);
