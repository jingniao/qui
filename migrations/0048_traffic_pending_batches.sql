CREATE TABLE traffic_pending_batches (
 node_id TEXT NOT NULL,
 batch_id TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64),
 encrypted_batch BLOB NOT NULL CHECK(length(encrypted_batch)>0 AND length(encrypted_batch)<=1049600),
 created_at TEXT NOT NULL,
 last_attempt_at TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 last_code TEXT NOT NULL DEFAULT '' CHECK(last_code IN ('','accounting_pending','material_invalid','source_unavailable','identity_conflict','statistics_pending','authorization_conflict','storage_failure','journal_unavailable')),
 PRIMARY KEY(node_id,batch_id)
);
CREATE INDEX traffic_pending_retry ON traffic_pending_batches(last_attempt_at,created_at,node_id,batch_id);