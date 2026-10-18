CREATE TABLE users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','disabled')),
 expires_at TEXT NOT NULL DEFAULT '', traffic_limit INTEGER NOT NULL DEFAULT 0 CHECK(traffic_limit>=0),
 used_traffic INTEGER NOT NULL DEFAULT 0 CHECK(used_traffic>=0),
 max_devices INTEGER NOT NULL CHECK(max_devices BETWEEN 1 AND 100),
 max_connections INTEGER NOT NULL CHECK(max_connections BETWEEN 1 AND 10000),
 protocols_json TEXT NOT NULL, remark TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE devices (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('active','disabled')),
 created_at TEXT NOT NULL, last_online_at TEXT NOT NULL DEFAULT '', last_node_id TEXT NOT NULL DEFAULT '',
 used_traffic INTEGER NOT NULL DEFAULT 0 CHECK(used_traffic>=0)
);
CREATE INDEX devices_user ON devices(user_id);
CREATE TABLE credentials (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 protocol TEXT NOT NULL, username TEXT NOT NULL DEFAULT '', encrypted_secret BLOB NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','revoked')), created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX credential_per_device_protocol ON credentials(device_id,protocol) WHERE status='active';
CREATE TABLE subscription_tokens (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, revoked_at TEXT,
 last_fetched_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX subscription_active_user ON subscription_tokens(user_id) WHERE revoked_at IS NULL;
CREATE TABLE node_groups (
 id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE,
 purpose TEXT NOT NULL CHECK(purpose IN ('subscription','relay','egress','mixed')),
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 subscription_visible INTEGER NOT NULL DEFAULT 1 CHECK(subscription_visible IN (0,1)),
 remark TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE node_group_members (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,node_id)
);
CREATE TABLE user_node_group_grants (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 PRIMARY KEY(user_id,group_id)
);
ALTER TABLE nodes ADD COLUMN allow_subscription INTEGER NOT NULL DEFAULT 1 CHECK(allow_subscription IN (0,1));
ALTER TABLE nodes ADD COLUMN maintenance INTEGER NOT NULL DEFAULT 0 CHECK(maintenance IN (0,1));
