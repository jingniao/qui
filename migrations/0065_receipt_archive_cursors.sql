CREATE TABLE receipt_archive_cursors (
 node_id TEXT NOT NULL REFERENCES nodes(id),
 kind TEXT NOT NULL CHECK(kind IN ('access','traffic')),
 first_id TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 0 CHECK(version>=0),
 updated_at TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(node_id,kind)
);
