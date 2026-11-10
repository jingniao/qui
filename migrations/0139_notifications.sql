CREATE TABLE notification_rules (
    kind TEXT PRIMARY KEY CHECK(kind IN ('node_offline','cpu','memory','disk','public_ip','machine_traffic','system_alert')),
    enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
    threshold REAL NOT NULL DEFAULT 0,
    duration_minutes INTEGER NOT NULL DEFAULT 0,
    cooldown_minutes INTEGER NOT NULL DEFAULT 30,
    updated_at TEXT NOT NULL
);
CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('telegram','webhook')),
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
    encrypted_config BLOB NOT NULL,
    last4 TEXT NOT NULL DEFAULT '',
    last_result_code TEXT NOT NULL DEFAULT '',
    last_sent_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE notifications (
    id INTEGER PRIMARY KEY,
    at TEXT NOT NULL,
    kind TEXT NOT NULL,
    severity TEXT NOT NULL CHECK(severity IN ('info','warning','critical','resolved')),
    node_id TEXT,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    read_at TEXT
);
CREATE INDEX notifications_at ON notifications(at);
CREATE TABLE notification_state (
    dedupe_key TEXT PRIMARY KEY,
    active INTEGER NOT NULL CHECK(active IN (0,1)),
    since TEXT NOT NULL,
    last_notified_at TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT ''
);
INSERT INTO notification_rules(kind,enabled,threshold,duration_minutes,cooldown_minutes,updated_at) VALUES
 ('node_offline',1,0,3,30,strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('cpu',1,90,5,30,strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('memory',1,90,5,30,strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('disk',1,90,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('public_ip',1,0,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('machine_traffic',1,100,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('system_alert',1,0,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
