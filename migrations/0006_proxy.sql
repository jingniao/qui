CREATE TABLE inbounds (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 protocol TEXT NOT NULL, listen TEXT NOT NULL, port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), subscription_visible INTEGER NOT NULL CHECK(subscription_visible IN (0,1)),
 internal_relay INTEGER NOT NULL CHECK(internal_relay IN (0,1)),
 default_outbound TEXT NOT NULL, allowed_users_json TEXT NOT NULL, encrypted_settings BLOB NOT NULL,
 relay_credential_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX inbound_active_port ON inbounds(node_id,port) WHERE enabled=1;
CREATE TABLE relay_credentials (
 id TEXT PRIMARY KEY, protocol TEXT NOT NULL, username TEXT NOT NULL DEFAULT '', encrypted_secret BLOB NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE outbounds (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id), protocol TEXT NOT NULL,
 address TEXT NOT NULL DEFAULT '', port INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 target_node_id TEXT REFERENCES nodes(id), target_inbound_id TEXT REFERENCES inbounds(id),
 credential_id TEXT NOT NULL DEFAULT '', encrypted_settings BLOB NOT NULL,
 support_tcp INTEGER NOT NULL CHECK(support_tcp IN (0,1)), support_udp INTEGER NOT NULL CHECK(support_udp IN (0,1)),
 failure_policy TEXT NOT NULL CHECK(failure_policy IN ('Block','Direct','BackupOutbound')),
 backup_outbound_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
