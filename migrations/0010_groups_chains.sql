CREATE TABLE outbound_groups (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 strategy TEXT NOT NULL CHECK(strategy IN ('primary','round_robin','weighted','latency')),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), probe_url TEXT NOT NULL,
 probe_interval INTEGER NOT NULL CHECK(probe_interval BETWEEN 3 AND 300),
 failure_policy TEXT NOT NULL CHECK(failure_policy IN ('Block','Direct','BackupOutbound')),
 backup_outbound_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE outbound_group_members (
 group_id TEXT NOT NULL REFERENCES outbound_groups(id) ON DELETE CASCADE,
 outbound_id TEXT NOT NULL REFERENCES outbounds(id),
 priority INTEGER NOT NULL CHECK(priority BETWEEN 0 AND 1000),
 weight INTEGER NOT NULL CHECK(weight BETWEEN 1 AND 100),
 PRIMARY KEY(group_id,outbound_id)
);
CREATE TABLE proxy_chains (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, node_ids_json TEXT NOT NULL,
 final_outbound_id TEXT NOT NULL, support_tcp INTEGER NOT NULL, support_udp INTEGER NOT NULL,
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 failure_policy TEXT NOT NULL CHECK(failure_policy IN ('Block','Direct','BackupOutbound','BackupChain')),
 backup_target_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE proxy_chain_hops (
 chain_id TEXT NOT NULL REFERENCES proxy_chains(id) ON DELETE CASCADE,
 position INTEGER NOT NULL, source_node_id TEXT NOT NULL REFERENCES nodes(id),
 target_node_id TEXT NOT NULL REFERENCES nodes(id), outbound_id TEXT NOT NULL REFERENCES outbounds(id),
 target_inbound_id TEXT NOT NULL REFERENCES inbounds(id), credential_id TEXT NOT NULL REFERENCES relay_credentials(id),
 protocol TEXT NOT NULL, failure_policy TEXT NOT NULL,
 PRIMARY KEY(chain_id,position)
);
CREATE TABLE outbound_status (
 outbound_id TEXT PRIMARY KEY REFERENCES outbounds(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id), healthy INTEGER NOT NULL,
 latency_ms INTEGER NOT NULL, checked_at TEXT NOT NULL
);
