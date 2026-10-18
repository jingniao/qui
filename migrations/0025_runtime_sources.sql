CREATE TABLE runtime_sources (
 node_id TEXT NOT NULL REFERENCES nodes(id),
 store_id TEXT NOT NULL,
 sequence INTEGER NOT NULL,
 epoch TEXT NOT NULL,
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 digest TEXT NOT NULL,
 encrypted_source BLOB NOT NULL,
 PRIMARY KEY(node_id,store_id,sequence),
 UNIQUE(node_id,epoch)
);
