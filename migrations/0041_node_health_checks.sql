ALTER TABLE nodes ADD COLUMN health_check_supported INTEGER NOT NULL DEFAULT 0 CHECK(health_check_supported IN (0,1));
CREATE TABLE node_health_checks (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 status TEXT NOT NULL CHECK(status IN ('queued','complete','expired')),
 created_at TEXT NOT NULL,
 deadline TEXT NOT NULL,
 created_by TEXT NOT NULL,
 result_json TEXT
);
CREATE UNIQUE INDEX node_health_check_pending ON node_health_checks(node_id) WHERE status='queued';
