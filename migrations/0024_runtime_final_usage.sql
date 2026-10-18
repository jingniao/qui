CREATE TABLE runtime_final_usage (
 node_id TEXT NOT NULL REFERENCES nodes(id),
 epoch TEXT NOT NULL,
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 digest TEXT NOT NULL,
 encrypted_usage BLOB NOT NULL,
 PRIMARY KEY(node_id,epoch)
);
CREATE INDEX runtime_final_usage_revision ON runtime_final_usage(node_id,revision);
