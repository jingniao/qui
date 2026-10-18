-- Keep legacy constrained columns and all existing references intact. New
-- modes are stored separately; empty means use the legacy mode.
ALTER TABLE outbounds ADD COLUMN failure_policy_v2 TEXT NOT NULL DEFAULT ''
 CHECK(failure_policy_v2 IN ('','Block','Direct','BackupOutbound','BackupOutboundGroup','BackupProxyChain'));
ALTER TABLE outbound_groups ADD COLUMN failure_policy_v2 TEXT NOT NULL DEFAULT ''
 CHECK(failure_policy_v2 IN ('','Block','Direct','BackupOutbound','BackupOutboundGroup','BackupProxyChain'));
ALTER TABLE proxy_chains ADD COLUMN failure_policy_v2 TEXT NOT NULL DEFAULT ''
 CHECK(failure_policy_v2 IN ('','Block','Direct','BackupOutbound','BackupOutboundGroup','BackupProxyChain','BackupChain'));
