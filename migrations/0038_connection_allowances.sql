CREATE TABLE user_connection_limits (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE user_node_connections (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 slots INTEGER NOT NULL CHECK(slots BETWEEN 1 AND 10000),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 status TEXT NOT NULL CHECK(status IN ('reserved','issued','retiring','closed')),
 closing_revision INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(user_id,node_id)
);
CREATE INDEX connection_user_outstanding ON user_node_connections(user_id,status);
