CREATE TABLE deployment_final_usage (
 revision INTEGER PRIMARY KEY REFERENCES config_revisions(id),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 stopped_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 digest TEXT NOT NULL,
 encrypted_usage BLOB NOT NULL
);
