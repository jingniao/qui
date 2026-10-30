ALTER TABLE probe_targets ADD COLUMN scheduled INTEGER NOT NULL DEFAULT 0 CHECK(scheduled IN (0,1));
ALTER TABLE node_tcp_probes ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK(source IN ('manual','scheduled'));
ALTER TABLE node_tcp_probes ADD COLUMN dispatched_at TEXT;
CREATE UNIQUE INDEX node_tcp_probes_inflight ON node_tcp_probes(node_id) WHERE status='queued' AND dispatched_at IS NOT NULL;
CREATE TRIGGER probe_targets_scheduled_insert BEFORE INSERT ON probe_targets
WHEN NEW.scheduled=1 AND (SELECT count(*) FROM probe_targets WHERE scheduled=1)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 scheduled probe targets'); END;
CREATE TRIGGER probe_targets_scheduled_update BEFORE UPDATE OF scheduled ON probe_targets
WHEN NEW.scheduled=1 AND OLD.scheduled=0 AND (SELECT count(*) FROM probe_targets WHERE scheduled=1)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 scheduled probe targets'); END;
-- Latencies are REAL milliseconds. The current Agent supplies only a median;
-- avg/min/max remain NULL until the wire contract can supply those statistics.
CREATE TABLE node_latency_samples (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES probe_targets(id) ON DELETE CASCADE,
    at TEXT NOT NULL,
    avg_ms REAL,
    min_ms REAL,
    max_ms REAL,
    sent INTEGER NOT NULL CHECK(sent BETWEEN 0 AND 3),
    failed INTEGER NOT NULL CHECK(failed BETWEEN 0 AND sent),
    median_ms REAL,
    PRIMARY KEY(node_id,target_id,at)
);
CREATE INDEX node_latency_samples_node_at ON node_latency_samples(node_id,at);
