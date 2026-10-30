CREATE TABLE node_metric_samples (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    at TEXT NOT NULL,
    cpu_percent REAL, memory_percent REAL, disk_percent REAL,
    memory_used_bytes INTEGER, swap_used_bytes INTEGER, disk_used_bytes INTEGER,
    load1 REAL, process_count INTEGER, tcp_connections INTEGER, udp_sockets INTEGER,
    active_connections INTEGER, rx_bytes_per_second REAL, tx_bytes_per_second REAL,
    PRIMARY KEY(node_id, at)
);
CREATE INDEX node_metric_samples_retention ON node_metric_samples(at);
CREATE TABLE node_tuning (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    desired_json TEXT NOT NULL,
    request_id TEXT,
    status TEXT NOT NULL CHECK(status IN ('pending','applied','cleared','failed','unconfirmed')),
    result_json TEXT,
    requested_at TEXT,
    updated_at TEXT
);
