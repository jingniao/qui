CREATE TABLE node_pause_tasks (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 previous_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 paused_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 current_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 previous_maintenance INTEGER NOT NULL CHECK(previous_maintenance IN (0,1)),
 status TEXT NOT NULL CHECK(status IN ('pausing','paused','failed','resuming','resume_failed','complete')),
 generation INTEGER NOT NULL CHECK(generation>0),
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX node_pause_active ON node_pause_tasks(node_id) WHERE status<>'complete';
CREATE TABLE node_pause_attempts (
 task_id TEXT NOT NULL REFERENCES node_pause_tasks(id),
 generation INTEGER NOT NULL CHECK(generation>0),
 operation TEXT NOT NULL CHECK(operation IN ('pause','resume')),
 revision INTEGER NOT NULL UNIQUE REFERENCES config_revisions(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(task_id,generation)
);
