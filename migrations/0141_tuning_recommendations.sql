ALTER TABLE node_metric_samples ADD COLUMN tcp_out_segs INTEGER;
ALTER TABLE node_metric_samples ADD COLUMN tcp_retrans_segs INTEGER;
ALTER TABLE node_meta ADD COLUMN bandwidth_mbps INTEGER CHECK(bandwidth_mbps IS NULL OR bandwidth_mbps BETWEEN 1 AND 100000);
CREATE TABLE node_tuning_dismissals (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, key TEXT NOT NULL, value TEXT NOT NULL,
 rules_version TEXT NOT NULL, dismissed_at TEXT NOT NULL, PRIMARY KEY(node_id, key));
