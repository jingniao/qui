CREATE TABLE core_traffic_hourly (
 node_id TEXT NOT NULL REFERENCES nodes(id), core TEXT NOT NULL CHECK(core IN ('xray','sing-box','gost')),
 bucket TEXT NOT NULL, upload_bytes INTEGER NOT NULL CHECK(typeof(upload_bytes)='integer' AND upload_bytes>=0),
 download_bytes INTEGER NOT NULL CHECK(typeof(download_bytes)='integer' AND download_bytes>=0),
 samples INTEGER NOT NULL CHECK(typeof(samples)='integer' AND samples>0),
 PRIMARY KEY(node_id,core,bucket)
);
CREATE INDEX core_traffic_hourly_time ON core_traffic_hourly(bucket);
CREATE TABLE traffic_storage_job (
 id INTEGER PRIMARY KEY CHECK(id=1), compacted_samples INTEGER NOT NULL DEFAULT 0,
 retired_receipts INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT '',
 last_success_at TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT ''
);
INSERT INTO traffic_storage_job(id) VALUES(1);
CREATE TABLE traffic_delivery_queues (
 node_id TEXT NOT NULL REFERENCES nodes(id), queue_id TEXT NOT NULL,
 completed_seq INTEGER NOT NULL DEFAULT 0, confirmed_seq INTEGER NOT NULL DEFAULT 0,
 retired_seq INTEGER NOT NULL DEFAULT 0, candidate_seq INTEGER NOT NULL DEFAULT 0,
 candidate_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
 closed_at TEXT NOT NULL DEFAULT '', recovery_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(node_id,queue_id),
 CHECK((closed_at='')=(recovery_id='')),
 CHECK(typeof(completed_seq)='integer' AND completed_seq>=0),
 CHECK(typeof(confirmed_seq)='integer' AND confirmed_seq BETWEEN 0 AND completed_seq),
 CHECK(typeof(retired_seq)='integer' AND retired_seq BETWEEN 0 AND confirmed_seq),
 CHECK(typeof(candidate_seq)='integer' AND candidate_seq BETWEEN retired_seq AND confirmed_seq)
);
CREATE UNIQUE INDEX traffic_delivery_active_node ON traffic_delivery_queues(node_id) WHERE closed_at='';
ALTER TABLE traffic_batches ADD COLUMN queue_id TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_batches ADD COLUMN delivery_seq INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX traffic_batch_delivery ON traffic_batches(node_id,queue_id,delivery_seq) WHERE delivery_seq>0;
ALTER TABLE traffic_pending_batches ADD COLUMN queue_id TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_pending_batches ADD COLUMN delivery_seq INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX traffic_pending_delivery ON traffic_pending_batches(node_id,queue_id,delivery_seq) WHERE delivery_seq>0;

-- The consumed recovery record is the explicit authorization to close the old
-- queue. Keep it atomic with identity replacement, including enrollment retries.
CREATE TRIGGER traffic_delivery_identity_recovery
AFTER UPDATE OF status ON node_identity_recoveries
WHEN NEW.status='consumed' AND OLD.status='issued'
BEGIN
 UPDATE traffic_delivery_queues SET closed_at=NEW.consumed_at,recovery_id=NEW.id,updated_at=NEW.consumed_at
 WHERE node_id=NEW.node_id AND closed_at='';
END;
