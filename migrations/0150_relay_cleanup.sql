-- Reused inbounds become ordinary inbounds without changing their configuration.
DELETE FROM canvas_entry_bindings WHERE mode='reuse';

CREATE TABLE relay_cleanup_nodes (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
    last_code TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision_id INTEGER CHECK(revision_id IS NULL OR revision_id>0)
);

CREATE TABLE relay_cleanup_reconcile (
    id INTEGER PRIMARY KEY CHECK(id=1),
    completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1))
);
INSERT INTO relay_cleanup_reconcile(id,completed) VALUES(1,0);
