ALTER TABLE nodes ADD COLUMN tcp_probe_supported INTEGER NOT NULL DEFAULT 0 CHECK(tcp_probe_supported IN (0,1));
CREATE TABLE probe_targets (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
    allow_non_public INTEGER NOT NULL DEFAULT 0 CHECK(allow_non_public IN (0,1))
);
CREATE TABLE node_tcp_probes (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES probe_targets(id) ON DELETE CASCADE,
    command_json TEXT NOT NULL,
    result_json TEXT,
    created_at TEXT NOT NULL,
    deadline TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('queued','complete','expired'))
);
CREATE INDEX node_tcp_probes_history ON node_tcp_probes(node_id,target_id,created_at DESC,id DESC);
