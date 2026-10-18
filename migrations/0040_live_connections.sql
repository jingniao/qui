CREATE TABLE node_live_connections (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL,
 core_epoch TEXT NOT NULL,
 observed_at TEXT NOT NULL,
 received_at TEXT NOT NULL,
 scope TEXT NOT NULL,
 connections INTEGER NOT NULL CHECK(connections BETWEEN 0 AND 100000000)
);
CREATE TABLE node_live_connection_accounts (
 node_id TEXT NOT NULL REFERENCES node_live_connections(node_id) ON DELETE CASCADE,
 credential_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 device_id TEXT NOT NULL,
 connections INTEGER NOT NULL CHECK(connections BETWEEN 1 AND 100000000),
 PRIMARY KEY(node_id,credential_id)
);
CREATE INDEX live_connection_users ON node_live_connection_accounts(user_id,device_id);
