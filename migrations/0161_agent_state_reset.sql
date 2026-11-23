CREATE TABLE agent_state_resets (
 reset_id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id),
 previous_fingerprint TEXT NOT NULL UNIQUE CHECK(length(previous_fingerprint)=64),
 public_key_digest TEXT NOT NULL CHECK(length(public_key_digest)=64),
 target_sha256 TEXT NOT NULL CHECK(length(target_sha256)=64),
 agent_version TEXT NOT NULL,
 certificate_pem BLOB NOT NULL, ca_pem BLOB NOT NULL,
 fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),
 expires_at TEXT NOT NULL, sealed_at TEXT NOT NULL,
 discarded_usage TEXT NOT NULL DEFAULT 'unknown' CHECK(discarded_usage='unknown')
);
ALTER TABLE access_batches ADD COLUMN reset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_batches ADD COLUMN reset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_pending_batches ADD COLUMN reset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_delivery_queues ADD COLUMN reset_id TEXT NOT NULL DEFAULT '';
CREATE INDEX access_batches_reset ON access_batches(reset_id,node_id,batch_id) WHERE reset_id<>'';
CREATE INDEX traffic_batches_reset ON traffic_batches(reset_id,node_id,batch_id) WHERE reset_id<>'';
