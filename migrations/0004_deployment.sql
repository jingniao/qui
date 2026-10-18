CREATE TABLE config_revisions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, author TEXT NOT NULL, summary TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('draft','pending','success','failed','rollback')),
 created_at TEXT NOT NULL, encrypted_state BLOB NOT NULL
);
CREATE TABLE deployment_targets (
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 status TEXT NOT NULL CHECK(status IN ('pending','success','failed','rollback')),
 result_code TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
 PRIMARY KEY(revision,node_id)
);
CREATE INDEX deployment_pending ON deployment_targets(node_id,status,revision);

