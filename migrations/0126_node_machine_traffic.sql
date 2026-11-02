CREATE TABLE node_machine_traffic_settings (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    method TEXT NOT NULL DEFAULT 'sum' CHECK(method IN ('sum','outbound','max')),
    interface_name TEXT NOT NULL DEFAULT '',
    allowance_bytes INTEGER CHECK(allowance_bytes IS NULL OR (typeof(allowance_bytes)='integer' AND allowance_bytes BETWEEN 1 AND 9007199254740991)),
    reset_day INTEGER NOT NULL DEFAULT 1 CHECK(typeof(reset_day)='integer' AND reset_day BETWEEN 1 AND 31)
);
CREATE TABLE node_machine_traffic_daily (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    interface_name TEXT NOT NULL,
    local_date TEXT NOT NULL,
    rx_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(rx_bytes)='integer' AND rx_bytes>=0),
    tx_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(tx_bytes)='integer' AND tx_bytes>=0),
    PRIMARY KEY(node_id,interface_name,local_date)
);
CREATE TABLE node_machine_traffic_cursors (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    interface_name TEXT NOT NULL,
    boot_id TEXT NOT NULL,
    rx_bytes INTEGER NOT NULL CHECK(typeof(rx_bytes)='integer' AND rx_bytes>=0),
    tx_bytes INTEGER NOT NULL CHECK(typeof(tx_bytes)='integer' AND tx_bytes>=0),
    observed_at TEXT NOT NULL,
    PRIMARY KEY(node_id,interface_name)
);
