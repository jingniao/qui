-- v1.6.1 baseline: the schema and built-in rows that migrations 0001-0152 produced
-- (v1.6.0). Panels upgraded through v1.6.0 already have it; see internal/platform/db.
CREATE TABLE app_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    created_at TEXT NOT NULL
);
CREATE TABLE admins (
 id TEXT PRIMARY KEY,
 username TEXT NOT NULL UNIQUE,
 display_name TEXT NOT NULL DEFAULT '',
 role TEXT NOT NULL CHECK (role IN ('Owner', 'Viewer')),
 status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
 password_hash TEXT NOT NULL,
 created_at TEXT NOT NULL,
 last_login_at TEXT NOT NULL DEFAULT '',
 login_ip TEXT NOT NULL DEFAULT ''
);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 admin_id TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE login_attempts (
 id INTEGER PRIMARY KEY,
 ip TEXT NOT NULL,
 attempted_at TEXT NOT NULL
);
CREATE TABLE admin_operations (
 id INTEGER PRIMARY KEY,
 timestamp TEXT NOT NULL,
 operator TEXT NOT NULL,
 action TEXT NOT NULL,
 target TEXT NOT NULL,
 result TEXT NOT NULL CHECK (result IN ('成功','失败')),
 ip TEXT NOT NULL
);
CREATE TABLE system_settings (
 key TEXT PRIMARY KEY,
 value_json TEXT NOT NULL
);
CREATE TABLE nodes (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','online','degraded','offline','applying','rollback','disabled','error')),
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 hostname TEXT NOT NULL DEFAULT '',
 operating_system TEXT NOT NULL DEFAULT '',
 agent_version TEXT NOT NULL DEFAULT '',
 public_ip TEXT NOT NULL DEFAULT '',
 last_heartbeat TEXT NOT NULL DEFAULT '',
 last_sequence INTEGER NOT NULL DEFAULT 0,
 capabilities_json TEXT NOT NULL DEFAULT '{}',
 heartbeat_json TEXT NOT NULL DEFAULT '{}',
 created_at TEXT NOT NULL
, allow_subscription INTEGER NOT NULL DEFAULT 1 CHECK(allow_subscription IN (0,1)), maintenance INTEGER NOT NULL DEFAULT 0 CHECK(maintenance IN (0,1)), budget_retirement_supported INTEGER NOT NULL DEFAULT 0 CHECK(budget_retirement_supported IN (0,1)), health_check_supported INTEGER NOT NULL DEFAULT 0 CHECK(health_check_supported IN (0,1)), core_restart_supported INTEGER NOT NULL DEFAULT 0 CHECK(core_restart_supported IN (0,1)), outbound_source_ip TEXT NOT NULL DEFAULT '', outbound_interface TEXT NOT NULL DEFAULT '', host_source_ipv4 TEXT NOT NULL DEFAULT '', host_source_ipv6 TEXT NOT NULL DEFAULT '', deleted_at TEXT, tcp_probe_supported INTEGER NOT NULL DEFAULT 0 CHECK(tcp_probe_supported IN (0,1)), latency_targets_custom INTEGER NOT NULL DEFAULT 0 CHECK(latency_targets_custom IN (0,1)), multi_core_supported INTEGER NOT NULL DEFAULT 0 CHECK(multi_core_supported IN (0,1)), node_traffic_supported INTEGER NOT NULL DEFAULT 0 CHECK(node_traffic_supported IN (0,1)), gost_core_supported INTEGER NOT NULL DEFAULT 0 CHECK(gost_core_supported IN (0,1)), public_ip_manual INTEGER NOT NULL DEFAULT 0 CHECK(public_ip_manual IN (0,1)), connect_host TEXT NOT NULL DEFAULT '', public_addresses_json TEXT NOT NULL DEFAULT '[]', public_ip_selected TEXT NOT NULL DEFAULT '');
CREATE TABLE node_enrollment_tokens (
 token_hash TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 expires_at TEXT NOT NULL,
 consumed_at TEXT
, purpose TEXT NOT NULL DEFAULT 'enroll' CHECK(purpose IN ('enroll','recover')), bound_fingerprint TEXT NOT NULL DEFAULT '');
CREATE TABLE node_identities (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 certificate_pem TEXT NOT NULL,
 fingerprint TEXT NOT NULL UNIQUE,
 expires_at TEXT NOT NULL,
 revoked_at TEXT
);
CREATE TABLE config_revisions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, author TEXT NOT NULL, summary TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('draft','pending','success','failed','rollback')),
 created_at TEXT NOT NULL, encrypted_state BLOB NOT NULL
, inbound_enabled_json TEXT);
CREATE TABLE deployment_targets (
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 status TEXT NOT NULL CHECK(status IN ('pending','success','failed','rollback')),
 result_code TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
 PRIMARY KEY(revision,node_id)
);
CREATE TABLE users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','disabled')),
 expires_at TEXT NOT NULL DEFAULT '', traffic_limit INTEGER NOT NULL DEFAULT 0 CHECK(traffic_limit>=0),
 used_traffic INTEGER NOT NULL DEFAULT 0 CHECK(used_traffic>=0),
 max_devices INTEGER NOT NULL CHECK(max_devices BETWEEN 1 AND 100),
 max_connections INTEGER NOT NULL CHECK(max_connections BETWEEN 1 AND 10000),
 protocols_json TEXT NOT NULL, remark TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
, password_hash TEXT NOT NULL DEFAULT '', login_enabled INTEGER NOT NULL DEFAULT 0 CHECK(login_enabled IN (0,1)), last_login_at TEXT NOT NULL DEFAULT '', device_limit INTEGER NOT NULL DEFAULT 0 CHECK(device_limit BETWEEN 0 AND 100), traffic_reset_cycle TEXT NOT NULL DEFAULT 'none' CHECK(traffic_reset_cycle IN ('none','monthly','30days')), traffic_reset_anchor TEXT NOT NULL DEFAULT '', traffic_reset_at TEXT NOT NULL DEFAULT '');
CREATE TABLE devices (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('active','disabled')),
 created_at TEXT NOT NULL, last_online_at TEXT NOT NULL DEFAULT '', last_node_id TEXT NOT NULL DEFAULT '',
 used_traffic INTEGER NOT NULL DEFAULT 0 CHECK(used_traffic>=0)
);
CREATE TABLE credentials (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 protocol TEXT NOT NULL, username TEXT NOT NULL DEFAULT '', encrypted_secret BLOB NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','revoked')), created_at TEXT NOT NULL
);
CREATE TABLE subscription_tokens (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, revoked_at TEXT,
 last_fetched_at TEXT NOT NULL DEFAULT ''
, encrypted_token BLOB);
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
CREATE TABLE inbounds (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 protocol TEXT NOT NULL, listen TEXT NOT NULL, port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), subscription_visible INTEGER NOT NULL CHECK(subscription_visible IN (0,1)),
 internal_relay INTEGER NOT NULL CHECK(internal_relay IN (0,1)),
 default_outbound TEXT NOT NULL, allowed_users_json TEXT NOT NULL, encrypted_settings BLOB NOT NULL,
 relay_credential_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
, core TEXT NOT NULL DEFAULT '' CHECK(core IN ('','xray','sing-box','gost')), tls_source TEXT NOT NULL DEFAULT '' CHECK(tls_source IN ('','paste','self_signed','file','acme')), certificate_id TEXT REFERENCES certificates(id));
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
, probe_url TEXT NOT NULL DEFAULT 'https://www.gstatic.com/generate_204', probe_interval INTEGER NOT NULL DEFAULT 30, failure_policy_v2 TEXT NOT NULL DEFAULT ''
 CHECK(failure_policy_v2 IN ('','Block','Direct','BackupOutbound','BackupOutboundGroup','BackupProxyChain')), source_ip TEXT NOT NULL DEFAULT '', source_interface TEXT NOT NULL DEFAULT '');
CREATE TABLE relay_bindings (
 outbound_id TEXT PRIMARY KEY REFERENCES outbounds(id) ON DELETE CASCADE,
 inbound_id TEXT NOT NULL REFERENCES inbounds(id),
 credential_id TEXT NOT NULL REFERENCES relay_credentials(id),
 tunnel_address TEXT NOT NULL DEFAULT ''
, additional_address TEXT NOT NULL DEFAULT '');
CREATE TABLE wireguard_addresses (
 inbound_id TEXT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 address TEXT NOT NULL, additional_address TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(inbound_id,device_id), UNIQUE(inbound_id,address)
);
CREATE TABLE policies (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, priority INTEGER NOT NULL CHECK(priority BETWEEN 0 AND 1000000),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), scope_json TEXT NOT NULL, match_json TEXT NOT NULL,
 action_json TEXT NOT NULL, logging_json TEXT NOT NULL, timezone TEXT NOT NULL DEFAULT 'UTC',
 remark TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE service_sets (
 id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, builtin INTEGER NOT NULL CHECK(builtin IN (0,1)),
 version INTEGER NOT NULL DEFAULT 1, enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE outbound_groups (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 strategy TEXT NOT NULL CHECK(strategy IN ('primary','round_robin','weighted','latency')),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), probe_url TEXT NOT NULL,
 probe_interval INTEGER NOT NULL CHECK(probe_interval BETWEEN 3 AND 300),
 failure_policy TEXT NOT NULL CHECK(failure_policy IN ('Block','Direct','BackupOutbound')),
 backup_outbound_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
, failure_policy_v2 TEXT NOT NULL DEFAULT ''
 CHECK(failure_policy_v2 IN ('','Block','Direct','BackupOutbound','BackupOutboundGroup','BackupProxyChain')));
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
, failure_policy_v2 TEXT NOT NULL DEFAULT ''
 CHECK(failure_policy_v2 IN ('','Block','Direct','BackupOutbound','BackupOutboundGroup','BackupProxyChain','BackupChain')));
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
CREATE TABLE deployment_batches (
 id TEXT PRIMARY KEY, chain_id TEXT NOT NULL, name TEXT NOT NULL, author TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('draft','deploying','success','failed','rolling_back','rolled_back','rollback_failed')),
 created_at TEXT NOT NULL
, tunnel_publication_json TEXT, tunnel_finalized INTEGER NOT NULL DEFAULT 1
    CHECK(tunnel_finalized IN (0,1)));
CREATE TABLE deployment_batch_items (
 batch_id TEXT NOT NULL REFERENCES deployment_batches(id) ON DELETE CASCADE,
 position INTEGER NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 previous_revision INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('waiting','pending','success','failed','rollback_pending','rolled_back','rollback_failed')),
 rollback_command_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(batch_id,position), UNIQUE(batch_id,node_id)
);
CREATE TABLE node_runtime_state (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id), revision INTEGER NOT NULL REFERENCES config_revisions(id)
);
CREATE TABLE deployment_commands (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id), revision INTEGER NOT NULL,
 operation TEXT NOT NULL CHECK(operation IN ('rollback')),
 status TEXT NOT NULL CHECK(status IN ('pending','success','failed')),
 result_code TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE access_batches (
 node_id TEXT NOT NULL REFERENCES nodes(id), batch_id TEXT NOT NULL,
 digest TEXT NOT NULL, received_at TEXT NOT NULL,
 PRIMARY KEY(node_id,batch_id)
);
CREATE TABLE traffic_hourly (
 bucket TEXT NOT NULL,node_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0),download_bytes INTEGER NOT NULL CHECK(download_bytes>=0),
 PRIMARY KEY(bucket,node_id,user_id,device_id)
);
CREATE TABLE traffic_daily (
 bucket TEXT NOT NULL,node_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0),download_bytes INTEGER NOT NULL CHECK(download_bytes>=0),
 PRIMARY KEY(bucket,node_id,user_id,device_id)
);
CREATE TABLE traffic_batches(node_id TEXT NOT NULL REFERENCES nodes(id),batch_id TEXT NOT NULL,digest TEXT NOT NULL,received_at TEXT NOT NULL, accounting_completed_at TEXT NOT NULL DEFAULT '',PRIMARY KEY(node_id,batch_id));
CREATE TABLE traffic_cursors(node_id TEXT NOT NULL,epoch TEXT NOT NULL,credential_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,observed_at TEXT NOT NULL, user_id TEXT NOT NULL DEFAULT '', device_id TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(node_id,epoch,credential_id));
CREATE TABLE account_traffic_hourly(bucket TEXT NOT NULL,node_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(bucket,node_id,user_id,device_id));
CREATE TABLE account_traffic_daily(bucket TEXT NOT NULL,node_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(bucket,node_id,user_id,device_id));
CREATE TABLE account_traffic_samples(node_id TEXT NOT NULL,epoch TEXT NOT NULL,credential_id TEXT NOT NULL,observed_at TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(node_id,epoch,credential_id,observed_at));
CREATE TABLE user_traffic_cursors(node_id TEXT NOT NULL,epoch TEXT NOT NULL,credential_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(node_id,epoch,credential_id));
CREATE TABLE backups (
 id TEXT PRIMARY KEY,
 created_at TEXT NOT NULL,
 created_by TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('manual','daily','weekly','monthly')),
 format_version INTEGER NOT NULL CHECK(format_version=1),
 file_bytes INTEGER NOT NULL CHECK(file_bytes>0),
 sha256 TEXT NOT NULL CHECK(length(sha256)=64),
 verified_at TEXT NOT NULL
, origin TEXT NOT NULL DEFAULT 'local' CHECK(origin IN ('local','imported')));
CREATE TABLE backup_runs (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('daily','weekly','monthly')),
 period_key TEXT NOT NULL,
 schedule_revision INTEGER NOT NULL CHECK(schedule_revision>0),
 status TEXT NOT NULL CHECK(status IN ('running','success','failed')),
 attempts INTEGER NOT NULL CHECK(attempts>0),
 backup_id TEXT REFERENCES backups(id) ON DELETE SET NULL,
 claim_token TEXT NOT NULL,
 lease_until TEXT NOT NULL,
 last_attempt_at TEXT NOT NULL,
 retry_at TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 UNIQUE(kind,period_key)
);
CREATE TABLE backup_file_ops (
 backup_id TEXT PRIMARY KEY,
 operation TEXT NOT NULL CHECK(operation IN ('create','delete')),
 phase TEXT NOT NULL CHECK(phase IN ('prepared','committed')),
 metadata_json TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE node_certificate_renewals (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 previous_fingerprint TEXT NOT NULL,
 certificate_pem TEXT NOT NULL,
 fingerprint TEXT NOT NULL UNIQUE,
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE release_catalogue (
 id TEXT PRIMARY KEY,
 component TEXT NOT NULL CHECK(component IN ('proxy-agent','xray','sing-box')),
 operating_system TEXT NOT NULL CHECK(operating_system IN ('linux','windows')),
 architecture TEXT NOT NULL CHECK(architecture IN ('amd64','arm64')),
 release_sequence INTEGER NOT NULL CHECK(release_sequence>0),
 version TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64),
 signed_json BLOB NOT NULL,
 signing_public_key TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(component,operating_system,architecture,release_sequence)
);
CREATE TABLE node_update_capabilities (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id),
 core_supported INTEGER NOT NULL CHECK(core_supported IN (0,1))
, agent_supported INTEGER NOT NULL DEFAULT 0 CHECK(agent_supported IN (0,1)), agent_recovery_supported INTEGER NOT NULL DEFAULT 0 CHECK(agent_recovery_supported IN (0,1)));
CREATE TABLE agent_update_tasks (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 release_id TEXT NOT NULL REFERENCES release_catalogue(id),
 baseline_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('queued','running','success','rolled_back','rejected','failed_closed')),
 generation INTEGER NOT NULL CHECK(generation>0),
 command_id TEXT NOT NULL UNIQUE,
 original_command_id TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 result_json BLOB
);
CREATE TABLE agent_update_attempts (
 command_id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES agent_update_tasks(id),
 generation INTEGER NOT NULL CHECK(generation>0),
 operation TEXT NOT NULL CHECK(operation IN ('install','recover')),
 request_json BLOB NOT NULL,
 result_json BLOB,
 created_at TEXT NOT NULL,
 UNIQUE(task_id,generation)
);
CREATE TABLE node_health_checks (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 status TEXT NOT NULL CHECK(status IN ('queued','complete','expired')),
 created_at TEXT NOT NULL,
 deadline TEXT NOT NULL,
 created_by TEXT NOT NULL,
 result_json TEXT
, core TEXT NOT NULL DEFAULT '');
CREATE TABLE node_restart_requests (
 command_id TEXT PRIMARY KEY REFERENCES deployment_commands(id),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 created_at TEXT NOT NULL
, core TEXT NOT NULL DEFAULT '');
CREATE TABLE traffic_pending_batches (
 node_id TEXT NOT NULL,
 batch_id TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(length(digest)=64),
 encrypted_batch BLOB NOT NULL CHECK(length(encrypted_batch)>0 AND length(encrypted_batch)<=1049600),
 created_at TEXT NOT NULL,
 last_attempt_at TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 last_code TEXT NOT NULL DEFAULT '' CHECK(last_code IN ('','accounting_pending','material_invalid','source_unavailable','identity_conflict','statistics_pending','authorization_conflict','storage_failure','journal_unavailable')),
 PRIMARY KEY(node_id,batch_id)
);
CREATE TABLE access_retention_job (
 id INTEGER PRIMARY KEY CHECK(id=1),
 generation INTEGER NOT NULL DEFAULT 0 CHECK(generation>=0),
 phase INTEGER NOT NULL DEFAULT 0 CHECK(phase BETWEEN 0 AND 5),
 status TEXT NOT NULL DEFAULT 'idle' CHECK(status IN ('idle','running','error','success')),
 policy_json TEXT NOT NULL DEFAULT '{}',
 cutoff_at TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT '',
 last_success_at TEXT NOT NULL DEFAULT '',
 deleted_run INTEGER NOT NULL DEFAULT 0 CHECK(deleted_run>=0),
 deleted_total INTEGER NOT NULL DEFAULT 0 CHECK(deleted_total>=0),
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
 error_code TEXT NOT NULL DEFAULT ''
);
CREATE TABLE access_space_maintenance (
 id INTEGER PRIMARY KEY CHECK(id=1),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 unconfirmed_attempts INTEGER NOT NULL DEFAULT 0 CHECK(unconfirmed_attempts>=0),
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
 status TEXT NOT NULL DEFAULT 'idle', mode TEXT NOT NULL DEFAULT 'unknown',
 started_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
 last_success_at TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT ''
);
CREATE TABLE node_accepted_messages (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 digest TEXT NOT NULL
);
CREATE TABLE node_enrollment_receipts (
 token_hash TEXT PRIMARY KEY CHECK(length(token_hash)=64),
 node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE RESTRICT,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 public_key_digest TEXT NOT NULL CHECK(length(public_key_digest)=64),
 hostname TEXT NOT NULL CHECK(length(hostname)<=128),
 operating_system TEXT NOT NULL CHECK(length(operating_system)<=256),
 agent_version TEXT NOT NULL CHECK(length(agent_version)<=64),
 certificate_pem TEXT NOT NULL CHECK(length(certificate_pem)<=32768),
 certificate_fingerprint TEXT NOT NULL CHECK(length(certificate_fingerprint)=64),
 ca_pem TEXT NOT NULL CHECK(length(ca_pem)<=32768),
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE TABLE access_rollups (
 id TEXT PRIMARY KEY, bucket TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 user_id TEXT NOT NULL DEFAULT '', device_id TEXT NOT NULL DEFAULT '',
 domain TEXT NOT NULL DEFAULT '', target_ip TEXT NOT NULL DEFAULT '',
 allowed INTEGER NOT NULL CHECK(allowed IN (0,1)), dimensions_json TEXT NOT NULL,
 connections INTEGER NOT NULL CHECK(connections>=0),
 upload_bytes INTEGER CHECK(upload_bytes IS NULL OR upload_bytes>=0),
 download_bytes INTEGER CHECK(download_bytes IS NULL OR download_bytes>=0),
 traffic_known INTEGER NOT NULL CHECK(traffic_known IN (0,1))
);
CREATE TABLE credential_tombstones (
  credential_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  deleted_at TEXT NOT NULL
);
CREATE TABLE canvas_schemes(
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    execution TEXT NOT NULL CHECK(execution IN ('server','client')),
    layout_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_at TEXT NOT NULL DEFAULT '',
    published_batch_id TEXT NOT NULL DEFAULT ''
, client_published_layout TEXT NOT NULL DEFAULT '');
CREATE TABLE user_sessions(
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE subscription_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url_encrypted BLOB NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 60 AND 2592000),
    last_fetch_at TEXT NOT NULL DEFAULT '',
    last_status TEXT NOT NULL DEFAULT 'pending' CHECK(last_status IN ('pending','ok','failed')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
, latest_result_json TEXT NOT NULL DEFAULT '', last_success_at TEXT NOT NULL DEFAULT '');
CREATE TABLE imported_proxies (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES subscription_sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL,
    payload_encrypted BLOB NOT NULL,
    fetched_at TEXT NOT NULL
, revision INTEGER NOT NULL DEFAULT 1);
CREATE TABLE user_source_grants (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES subscription_sources(id) ON DELETE CASCADE,
    PRIMARY KEY(user_id, source_id)
);
CREATE TABLE node_meta (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 region TEXT NOT NULL DEFAULT '', remark TEXT NOT NULL DEFAULT '',
 renew_date TEXT NOT NULL DEFAULT '', renew_cycle TEXT NOT NULL DEFAULT '',
 renew_price TEXT NOT NULL DEFAULT '', renew_currency TEXT NOT NULL DEFAULT '',
 renew_notice_days INTEGER NOT NULL DEFAULT 7 CHECK(renew_notice_days BETWEEN 0 AND 365),
 last_alert_day TEXT NOT NULL DEFAULT ''
, port_range_start INTEGER, port_range_end INTEGER CHECK(
 (port_range_start IS NULL AND port_range_end IS NULL) OR
 (port_range_start IS NOT NULL AND port_range_end IS NOT NULL AND
 typeof(port_range_start)='integer' AND typeof(port_range_end)='integer' AND
 port_range_start BETWEEN 1 AND port_range_end AND port_range_end<=65535)), bandwidth_mbps INTEGER CHECK(bandwidth_mbps IS NULL OR bandwidth_mbps BETWEEN 1 AND 100000), tags_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(tags_json) AND json_type(tags_json)='array'));
CREATE TABLE probe_targets (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
    allow_non_public INTEGER NOT NULL DEFAULT 0 CHECK(allow_non_public IN (0,1))
, scheduled INTEGER NOT NULL DEFAULT 0 CHECK(scheduled IN (0,1)));
CREATE TABLE node_tcp_probes (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES probe_targets(id) ON DELETE CASCADE,
    command_json TEXT NOT NULL,
    result_json TEXT,
    created_at TEXT NOT NULL,
    deadline TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('queued','complete','expired'))
, source TEXT NOT NULL DEFAULT 'manual' CHECK(source IN ('manual','scheduled')), dispatched_at TEXT);
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
CREATE TABLE node_latency_targets (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES probe_targets(id) ON DELETE CASCADE,
    PRIMARY KEY(node_id,target_id)
);
CREATE TABLE node_group_servers (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,node_id)
);
CREATE TABLE node_group_inbounds (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 inbound_id TEXT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,inbound_id)
);
CREATE TABLE node_group_imports (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL REFERENCES subscription_sources(id) ON DELETE CASCADE,
 proxy_key TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(group_id,source_id,proxy_key)
);
CREATE TABLE node_metric_samples (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    at TEXT NOT NULL,
    cpu_percent REAL, memory_percent REAL, disk_percent REAL,
    memory_used_bytes INTEGER, swap_used_bytes INTEGER, disk_used_bytes INTEGER,
    load1 REAL, process_count INTEGER, tcp_connections INTEGER, udp_sockets INTEGER,
    active_connections INTEGER, rx_bytes_per_second REAL, tx_bytes_per_second REAL, tcp_out_segs INTEGER, tcp_retrans_segs INTEGER,
    PRIMARY KEY(node_id, at)
);
CREATE TABLE node_tuning (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    desired_json TEXT NOT NULL,
    request_id TEXT,
    status TEXT NOT NULL CHECK(status IN ('pending','applied','cleared','failed','unconfirmed')),
    result_json TEXT,
    requested_at TEXT,
    updated_at TEXT
);
CREATE TABLE canvas_tunnels (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 connection_json TEXT NOT NULL CHECK(json_valid(connection_json)),
 source_ref TEXT NOT NULL,
 target_ref TEXT NOT NULL,
 inline_scheme_id TEXT NOT NULL DEFAULT '',
 object_prefix TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 published_version TEXT NOT NULL DEFAULT '',
 allocated_port INTEGER CHECK(allocated_port IS NULL OR allocated_port BETWEEN 1 AND 65535)
, gost_material BLOB);
CREATE TABLE canvas_tunnel_objects (
 tunnel_id TEXT NOT NULL REFERENCES canvas_tunnels(id) ON DELETE RESTRICT,
 object_kind TEXT NOT NULL CHECK(object_kind IN ('inbound','outbound','chain')),
 object_id TEXT NOT NULL,
 PRIMARY KEY(object_kind,object_id)
);
CREATE TABLE canvas_tunnel_publications (
 scheme_id TEXT NOT NULL REFERENCES canvas_schemes(id) ON DELETE CASCADE,
 tunnel_id TEXT NOT NULL REFERENCES canvas_tunnels(id) ON DELETE CASCADE,
 PRIMARY KEY(scheme_id,tunnel_id)
);
CREATE TABLE canvas_tunnel_reconcile (id INTEGER PRIMARY KEY CHECK(id=1), done INTEGER NOT NULL CHECK(done IN(0,1)));
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
CREATE TABLE "core_update_tasks" (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id),
  baseline_revision INTEGER NOT NULL REFERENCES config_revisions(id),
  release_id TEXT REFERENCES release_catalogue(id),
  component TEXT NOT NULL CHECK(component IN ('xray','sing-box','gost')),
  version TEXT NOT NULL,
  url TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64),
  sequence INTEGER NOT NULL CHECK(sequence>0),
  status TEXT NOT NULL CHECK(status IN ('queued','running','success','rolled_back','rejected','failed_closed','cleared')),
  command_id TEXT NOT NULL UNIQUE,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  result_json BLOB,
  adding INTEGER NOT NULL DEFAULT 0 CHECK(adding IN (0,1))
);
CREATE TABLE "core_remove_tasks" (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id),
 component TEXT NOT NULL CHECK(component IN ('xray','sing-box','gost')),
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('queued','running','success','rejected','unconfirmed')),
 code TEXT NOT NULL DEFAULT '', result_json BLOB,
 created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE "node_traffic_cursors" (
 node_id TEXT NOT NULL REFERENCES nodes(id), epoch TEXT NOT NULL, credential_id TEXT NOT NULL CHECK(credential_id='node'),
 core TEXT NOT NULL CHECK(core IN ('xray','sing-box','gost')), revision INTEGER NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), observed_at TEXT NOT NULL,
 PRIMARY KEY(node_id,epoch,credential_id)
);
CREATE TABLE "core_traffic_samples" (
 node_id TEXT NOT NULL REFERENCES nodes(id), core TEXT NOT NULL CHECK(core IN ('xray','sing-box','gost')), epoch TEXT NOT NULL, observed_at TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0),
 PRIMARY KEY(node_id,core,epoch,observed_at)
);
CREATE TABLE tunnels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE CHECK(length(trim(name)) BETWEEN 1 AND 80),
    transport TEXT NOT NULL CHECK(transport='tls'),
    status TEXT NOT NULL CHECK(status IN ('draft','modified','published','needs_publish','pending_delete')),
    draft_version INTEGER NOT NULL DEFAULT 1 CHECK(draft_version>0),
    published_version INTEGER NOT NULL DEFAULT 0 CHECK(published_version>=0),
    published_at TEXT,
    gost_material BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE tunnel_hops (
    tunnel_id TEXT NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK(role IN ('entry','mid','exit')),
    position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 2),
    node_id TEXT NOT NULL REFERENCES nodes(id),
    listen_port INTEGER CHECK(listen_port BETWEEN 1 AND 65535),
    port_mode TEXT NOT NULL CHECK(port_mode IN ('auto','manual')),
    connect_host TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(tunnel_id,role),
    UNIQUE(tunnel_id,position),
    UNIQUE(tunnel_id,node_id),
    CHECK((role='entry' AND position=0 AND listen_port IS NULL) OR (role<>'entry' AND position>0 AND listen_port IS NOT NULL))
);
CREATE TABLE port_claims (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
    owner_kind TEXT NOT NULL CHECK(owner_kind IN ('tunnel_hop','rule','canvas')),
    owner_id TEXT NOT NULL,
    PRIMARY KEY(node_id,port)
);
CREATE TABLE tunnel_objects (
    object_kind TEXT NOT NULL CHECK(object_kind='inbound'),
    object_id TEXT NOT NULL UNIQUE REFERENCES inbounds(id) ON DELETE CASCADE,
    owner_kind TEXT NOT NULL CHECK(owner_kind IN ('tunnel','rule')),
    owner_id TEXT NOT NULL,
    PRIMARY KEY(object_kind,object_id)
);
CREATE TABLE canvas_forward_reconcile (
    id INTEGER PRIMARY KEY CHECK(id=1),
    done INTEGER NOT NULL CHECK(done IN (0,1)),
    migrated_ids TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(migrated_ids) AND json_type(migrated_ids)='array')
);
CREATE TABLE tunnel_migration_report (
    tunnel_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    reason TEXT NOT NULL
);
CREATE TABLE client_policy_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    template_json TEXT NOT NULL,
    ui_metadata_json TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    import_key TEXT UNIQUE,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by TEXT NOT NULL
);
CREATE TABLE node_identity_recoveries (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 token_hash TEXT NOT NULL UNIQUE CHECK(length(token_hash)=64),
 previous_fingerprint TEXT NOT NULL CHECK(length(previous_fingerprint)=64),
 new_fingerprint TEXT NOT NULL DEFAULT '' CHECK(new_fingerprint='' OR length(new_fingerprint)=64),
 owner TEXT NOT NULL,
 revoked_on_issue INTEGER NOT NULL CHECK(revoked_on_issue IN (0,1)),
 status TEXT NOT NULL CHECK(status IN ('issued','consumed','superseded','expired')),
 redeploy_state TEXT NOT NULL DEFAULT 'none' CHECK(redeploy_state IN ('none','pending','queued','success','failed','not_needed')),
 redeploy_revision INTEGER NOT NULL DEFAULT 0,
 redeploy_code TEXT NOT NULL DEFAULT '' CHECK(length(redeploy_code)<=64),
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 consumed_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE node_identity_retirements (
 fingerprint TEXT PRIMARY KEY CHECK(length(fingerprint)=64),
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 recovery_id TEXT NOT NULL,
 retired_at TEXT NOT NULL
);
CREATE TABLE certificates (
    id TEXT PRIMARY KEY,
    domains_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending','issuing','valid','failed')),
    encrypted_material BLOB,
    not_before TEXT NOT NULL DEFAULT '',
    not_after TEXT NOT NULL DEFAULT '',
    last_error_code TEXT NOT NULL DEFAULT '',
    last_attempt_at TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
    auto_dns INTEGER NOT NULL DEFAULT 0 CHECK(auto_dns IN (0,1)),
    dns_node_id TEXT REFERENCES nodes(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
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
CREATE TABLE notification_state (
    dedupe_key TEXT PRIMARY KEY,
    active INTEGER NOT NULL CHECK(active IN (0,1)),
    since TEXT NOT NULL,
    last_notified_at TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE "service_set_entries" (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('domain','suffix','cidr','keyword')),
    value TEXT NOT NULL,
    UNIQUE(service_id,kind,value)
);
CREATE TABLE node_tuning_dismissals (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, key TEXT NOT NULL, value TEXT NOT NULL,
 rules_version TEXT NOT NULL, dismissed_at TEXT NOT NULL, PRIMARY KEY(node_id, key));
CREATE TABLE "service_set_sources" (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    format TEXT NOT NULL CHECK(format IN ('yaml','text','mrs','native')),
    behavior TEXT NOT NULL CHECK(behavior IN ('classical','domain','ipcidr')),
    interval_seconds INTEGER NOT NULL DEFAULT 86400 CHECK(interval_seconds BETWEEN 3600 AND 604800),
    position INTEGER NOT NULL,
    last_status TEXT NOT NULL DEFAULT 'pending' CHECK(last_status IN ('pending','ok','failed')),
    last_error_code TEXT NOT NULL DEFAULT '',
    last_fetch_at TEXT,
    last_success_at TEXT,
    entry_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    encrypted_cache BLOB,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    xray_url TEXT NOT NULL DEFAULT '',
    xray_tag TEXT NOT NULL DEFAULT '',
    CHECK(format <> 'mrs' OR behavior IN ('domain','ipcidr')),
    CHECK(format <> 'native' OR (behavior IN ('domain','ipcidr') AND xray_url <> '' AND xray_tag <> '' AND encrypted_cache IS NULL)),
    CHECK(format = 'native' OR (xray_url = '' AND xray_tag = ''))
);
CREATE TABLE canvas_entry_bindings (
 id TEXT PRIMARY KEY,
 scheme_id TEXT REFERENCES canvas_schemes(id) ON DELETE SET NULL,
 instance_id TEXT NOT NULL,
 inbound_id TEXT NOT NULL UNIQUE REFERENCES inbounds(id) ON DELETE RESTRICT,
 mode TEXT NOT NULL CHECK(mode IN ('create','reuse')),
 original_default_outbound TEXT NOT NULL DEFAULT '' CHECK(mode <> 'reuse' OR original_default_outbound <> ''),
 applied_default_outbound TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 request_id TEXT UNIQUE,
 updated_at TEXT NOT NULL, creation_config_json TEXT, creation_groups_json TEXT,
 UNIQUE(scheme_id,instance_id)
);
CREATE TABLE import_identity_reconcile (
    id INTEGER PRIMARY KEY CHECK(id=1),
    done INTEGER NOT NULL CHECK(done IN (0,1))
);
CREATE TABLE import_identity_map (
    source_id TEXT NOT NULL,
    old_proxy_id TEXT NOT NULL,
    old_proxy_key TEXT NOT NULL,
    new_proxy_id TEXT NOT NULL UNIQUE,
    PRIMARY KEY(source_id,old_proxy_id)
);
CREATE TABLE relay_cleanup_nodes (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
    last_code TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision_id INTEGER CHECK(revision_id IS NULL OR revision_id>0)
);
CREATE TABLE relay_cleanup_reconcile (
    id INTEGER PRIMARY KEY CHECK(id=1),
    completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1))
);
CREATE TABLE node_group_relays (
    group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    rule_id TEXT NOT NULL REFERENCES tunnel_rules(id) ON DELETE CASCADE,
    PRIMARY KEY(group_id,rule_id)
);
CREATE TABLE "tunnel_rules" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 80),
    entry_node_id TEXT NOT NULL REFERENCES nodes(id),
    listen_port INTEGER NOT NULL CHECK(listen_port BETWEEN 1 AND 65535),
    port_mode TEXT NOT NULL CHECK(port_mode IN ('auto','manual')),
    network TEXT NOT NULL CHECK(network IN ('tcp','udp','both')),
    tunnel_id TEXT REFERENCES tunnels(id),
    target_mode TEXT NOT NULL CHECK(target_mode IN ('linked','manual','import')),
    target_inbound_id TEXT REFERENCES inbounds(id),
    target_host TEXT,
    target_port INTEGER CHECK(target_port BETWEEN 1 AND 65535),
    owner_node_id TEXT REFERENCES nodes(id),
    subscription_visible INTEGER NOT NULL CHECK(subscription_visible IN (0,1)),
    enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
    status TEXT NOT NULL CHECK(status IN ('draft','modified','published','needs_publish','pending_delete')),
    draft_version INTEGER NOT NULL DEFAULT 1 CHECK(draft_version>0),
    published_version INTEGER NOT NULL DEFAULT 0 CHECK(published_version>=0),
    published_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_snapshot TEXT,
    target_import_id TEXT REFERENCES imported_proxies(id) ON DELETE SET NULL,
    CHECK((target_mode='linked' AND target_inbound_id IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND target_import_id IS NULL) OR
          (target_mode='manual' AND target_inbound_id IS NULL AND length(target_host)>0 AND target_port IS NOT NULL AND subscription_visible=0 AND target_import_id IS NULL) OR
          (target_mode='import' AND target_inbound_id IS NULL AND target_host IS NULL AND target_port IS NULL))
);
INSERT INTO "app_metadata" VALUES(1,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "policies" VALUES('policy-block-china','屏蔽回国',1,0,'{}','{"service_ids":["service-cn"]}','{"type":"Block","failover":"Block"}','{"log_domain":true,"log_traffic":false,"alert_on_block":false}','UTC','内置：启用并发布后，节点拒绝访问国内域名与国内 IP',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-netflix','Netflix',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-disney','Disney+',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-youtube','YouTube',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-openai','OpenAI',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-google','Google',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-github','GitHub',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-telegram','Telegram',1,1,1,'内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-ads','广告拦截',1,1,1,'内置：广告与追踪域名（meta-rules-dat category-ads-all）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-ai','AI 服务',1,1,1,'内置：境外 AI 服务（meta-rules-dat category-ai-!cn、openai）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-microsoft','微软服务',1,1,1,'内置：微软（meta-rules-dat microsoft）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-apple','苹果服务',1,1,1,'内置：苹果与 Apple Music（meta-rules-dat apple、apple-music）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-social','社交媒体',1,1,1,'内置：Facebook、Instagram、Twitter、TikTok、LinkedIn',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-streaming','流媒体',1,1,1,'内置：Netflix、Hulu、Disney+、HBO、Amazon、巴哈姆特、哔哩哔哩国际版',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-games','游戏平台',1,1,1,'内置：Steam、Epic、EA、Ubisoft、暴雪',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-education','教育资源',1,1,1,'内置：Coursera、edX、Udemy、可汗学院与境外学术站点',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-finance','金融服务',1,1,1,'内置：PayPal、Visa、Mastercard、Stripe、Wise',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-cloud','云服务',1,1,1,'内置：AWS、Azure、DigitalOcean、Heroku、Dropbox、Cloudflare、OneDrive',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-spotify','Spotify',1,1,1,'内置：Spotify',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-pixiv','Pixiv',1,1,1,'内置：Pixiv',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-abema','Abema',1,1,1,'内置：AbemaTV',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-ehentai','E-Hentai',1,1,1,'内置：E-Hentai',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-gfw','代理服务',1,1,1,'内置：常见被屏蔽站点（meta-rules-dat gfw）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-media','代理媒体',1,1,1,'内置：境外媒体（meta-rules-dat category-media）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-cn','国内',1,1,1,'内置：国内域名与国内 IP（meta-rules-dat geolocation-cn 与 geoip cn；节点 Agent 支持时改用完整的 geosite cn 与 geoip cn 原生规则集），用于屏蔽回国',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_sets" VALUES('service-private','私有网络',1,1,1,'内置：私有与保留地址（meta-rules-dat geoip private）',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "access_retention_job" VALUES(1,0,0,'idle','{}','','','','',0,0,0,'');
INSERT INTO "access_space_maintenance" VALUES(1,0,0,0,'idle','unknown','','','','');
INSERT INTO "canvas_tunnel_reconcile" VALUES(1,1);
INSERT INTO "canvas_forward_reconcile" VALUES(1,1,'[]');
INSERT INTO "notification_rules" VALUES('node_offline',1,0.0,3,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "notification_rules" VALUES('cpu',1,90.0,5,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "notification_rules" VALUES('memory',1,90.0,5,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "notification_rules" VALUES('disk',1,90.0,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "notification_rules" VALUES('public_ip',1,0.0,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "notification_rules" VALUES('machine_traffic',1,100.0,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "notification_rules" VALUES('system_alert',1,0.0,0,30,strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO "service_set_entries" VALUES(1,'service-netflix','suffix','netflix.com');
INSERT INTO "service_set_entries" VALUES(2,'service-netflix','suffix','nflxvideo.net');
INSERT INTO "service_set_entries" VALUES(3,'service-netflix','suffix','nflximg.net');
INSERT INTO "service_set_entries" VALUES(4,'service-netflix','suffix','nflxext.com');
INSERT INTO "service_set_entries" VALUES(5,'service-disney','suffix','disneyplus.com');
INSERT INTO "service_set_entries" VALUES(6,'service-disney','suffix','dssott.com');
INSERT INTO "service_set_entries" VALUES(7,'service-disney','suffix','disney-plus.net');
INSERT INTO "service_set_entries" VALUES(8,'service-youtube','suffix','youtube.com');
INSERT INTO "service_set_entries" VALUES(9,'service-youtube','suffix','googlevideo.com');
INSERT INTO "service_set_entries" VALUES(10,'service-youtube','suffix','ytimg.com');
INSERT INTO "service_set_entries" VALUES(11,'service-youtube','suffix','youtu.be');
INSERT INTO "service_set_entries" VALUES(12,'service-openai','suffix','openai.com');
INSERT INTO "service_set_entries" VALUES(13,'service-openai','suffix','chatgpt.com');
INSERT INTO "service_set_entries" VALUES(14,'service-openai','suffix','oaistatic.com');
INSERT INTO "service_set_entries" VALUES(15,'service-openai','suffix','oaiusercontent.com');
INSERT INTO "service_set_entries" VALUES(16,'service-google','suffix','google.com');
INSERT INTO "service_set_entries" VALUES(17,'service-google','suffix','gstatic.com');
INSERT INTO "service_set_entries" VALUES(18,'service-google','suffix','googleapis.com');
INSERT INTO "service_set_entries" VALUES(19,'service-github','suffix','github.com');
INSERT INTO "service_set_entries" VALUES(20,'service-github','suffix','githubusercontent.com');
INSERT INTO "service_set_entries" VALUES(21,'service-github','suffix','githubassets.com');
INSERT INTO "service_set_entries" VALUES(22,'service-telegram','suffix','telegram.org');
INSERT INTO "service_set_entries" VALUES(23,'service-telegram','suffix','t.me');
INSERT INTO "service_set_entries" VALUES(24,'service-telegram','suffix','tdesktop.com');
INSERT INTO "service_set_sources" VALUES('builtin-service-netflix-0','service-netflix','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/netflix.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-netflix-1','service-netflix','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/netflix.yaml','yaml','ipcidr',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-disney-0','service-disney','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/disney.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-youtube-0','service-youtube','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/youtube.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-openai-0','service-openai','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/openai.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-google-0','service-google','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/google.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-google-1','service-google','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/google.yaml','yaml','ipcidr',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-github-0','service-github','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/github.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-github-1','service-github','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/gitlab.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-telegram-0','service-telegram','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/telegram.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-telegram-1','service-telegram','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/telegram.yaml','yaml','ipcidr',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-ads-0','service-ads','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/category-ads-all.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-ai-0','service-ai','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/category-ai-!cn.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-ai-1','service-ai','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/openai.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-microsoft-0','service-microsoft','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/microsoft.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-apple-0','service-apple','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/apple.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-apple-1','service-apple','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/apple-music.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-0','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/facebook.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-1','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/instagram.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-2','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/twitter.yaml','yaml','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-3','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/tiktok.yaml','yaml','domain',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-4','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/linkedin.yaml','yaml','domain',86400,4,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-5','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/facebook.yaml','yaml','ipcidr',86400,5,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-social-6','service-social','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/twitter.yaml','yaml','ipcidr',86400,6,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-0','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/netflix.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-1','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/hulu.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-2','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/disney.yaml','yaml','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-3','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/hbo.yaml','yaml','domain',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-4','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/amazon.yaml','yaml','domain',86400,4,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-5','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/bahamut.yaml','yaml','domain',86400,5,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-6','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/biliintl.yaml','yaml','domain',86400,6,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-streaming-7','service-streaming','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/netflix.yaml','yaml','ipcidr',86400,7,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-games-0','service-games','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/steam.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-games-1','service-games','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/epicgames.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-games-2','service-games','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/ea.yaml','yaml','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-games-3','service-games','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/ubisoft.yaml','yaml','domain',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-games-4','service-games','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/blizzard.yaml','yaml','domain',86400,4,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-education-0','service-education','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/coursera.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-education-1','service-education','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/edx.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-education-2','service-education','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/udemy.yaml','yaml','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-education-3','service-education','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/khanacademy.yaml','yaml','domain',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-education-4','service-education','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/category-scholar-!cn.yaml','yaml','domain',86400,4,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-finance-0','service-finance','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/paypal.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-finance-1','service-finance','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/visa.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-finance-2','service-finance','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/mastercard.yaml','yaml','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-finance-3','service-finance','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/stripe.yaml','yaml','domain',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-finance-4','service-finance','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/wise.yaml','yaml','domain',86400,4,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-0','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/aws.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-1','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/azure.yaml','yaml','domain',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-2','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/digitalocean.yaml','yaml','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-3','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/heroku.yaml','yaml','domain',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-4','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/dropbox.yaml','yaml','domain',86400,4,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-5','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/cloudflare.yaml','yaml','domain',86400,5,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cloud-6','service-cloud','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/onedrive.yaml','yaml','domain',86400,6,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-spotify-0','service-spotify','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/spotify.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-pixiv-0','service-pixiv','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/pixiv.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-abema-0','service-abema','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/abema.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-ehentai-0','service-ehentai','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/ehentai.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-gfw-0','service-gfw','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/gfw.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-media-0','service-media','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/category-media.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cn-0','service-cn','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/geolocation-cn.yaml','yaml','domain',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cn-1','service-cn','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/cn.yaml','yaml','ipcidr',86400,1,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-private-0','service-private','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/private.yaml','yaml','ipcidr',86400,0,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'','');
INSERT INTO "service_set_sources" VALUES('builtin-service-cn-native-1','service-cn','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs','native','domain',86400,2,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat','cn');
INSERT INTO "service_set_sources" VALUES('builtin-service-cn-native-2','service-cn','https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs','native','ipcidr',86400,3,'pending','',NULL,NULL,0,0,NULL,strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'),'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat','cn');
INSERT INTO "import_identity_reconcile" VALUES(1,1);
INSERT INTO "relay_cleanup_reconcile" VALUES(1,1);
CREATE UNIQUE INDEX one_owner ON admins(role) WHERE role = 'Owner';
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE INDEX login_attempts_window ON login_attempts(ip, attempted_at);
CREATE INDEX admin_operations_time ON admin_operations(timestamp);
CREATE INDEX deployment_pending ON deployment_targets(node_id,status,revision);
CREATE INDEX devices_user ON devices(user_id);
CREATE UNIQUE INDEX credential_per_device_protocol ON credentials(device_id,protocol) WHERE status='active';
CREATE UNIQUE INDEX subscription_active_user ON subscription_tokens(user_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX relay_tunnel_address ON relay_bindings(inbound_id,tunnel_address) WHERE tunnel_address<>'';
CREATE INDEX policy_priority ON policies(priority,id);
CREATE INDEX account_traffic_time ON account_traffic_samples(observed_at);
CREATE INDEX backups_created_at ON backups(created_at);
CREATE INDEX backup_runs_status ON backup_runs(status,last_attempt_at);
CREATE UNIQUE INDEX agent_update_active_node ON agent_update_tasks(node_id) WHERE status IN ('queued','running','failed_closed');
CREATE UNIQUE INDEX node_health_check_pending ON node_health_checks(node_id) WHERE status='queued';
CREATE UNIQUE INDEX wireguard_additional_address ON wireguard_addresses(inbound_id,additional_address) WHERE additional_address<>'';
CREATE UNIQUE INDEX relay_additional_address ON relay_bindings(inbound_id,additional_address) WHERE additional_address<>'';
CREATE INDEX traffic_pending_retry ON traffic_pending_batches(last_attempt_at,created_at,node_id,batch_id);
CREATE INDEX access_time ON access_rollups(bucket DESC,id);
CREATE INDEX access_node_time ON access_rollups(node_id,bucket DESC);
CREATE INDEX access_user_time ON access_rollups(user_id,bucket DESC);
CREATE INDEX access_domain_time ON access_rollups(domain,bucket DESC);
CREATE INDEX access_blocked_time ON access_rollups(allowed,bucket DESC);
CREATE INDEX credential_tombstones_identity ON credential_tombstones(user_id,device_id);
CREATE INDEX user_sessions_expiry ON user_sessions(expires_at);
CREATE INDEX node_tcp_probes_history ON node_tcp_probes(node_id,target_id,created_at DESC,id DESC);
CREATE UNIQUE INDEX node_tcp_probes_inflight ON node_tcp_probes(node_id) WHERE status='queued' AND dispatched_at IS NOT NULL;
CREATE INDEX node_latency_samples_node_at ON node_latency_samples(node_id,at);
CREATE INDEX node_group_servers_node ON node_group_servers(node_id);
CREATE INDEX node_group_inbounds_inbound ON node_group_inbounds(inbound_id);
CREATE INDEX node_metric_samples_retention ON node_metric_samples(at);
CREATE INDEX canvas_tunnel_objects_owner ON canvas_tunnel_objects(tunnel_id);
CREATE UNIQUE INDEX core_update_active_component ON core_update_tasks(node_id,component) WHERE status IN ('queued','running');
CREATE UNIQUE INDEX core_remove_active_component ON core_remove_tasks(node_id,component) WHERE status IN ('queued','running');
CREATE INDEX core_traffic_time ON core_traffic_samples(observed_at);
CREATE INDEX port_claims_owner ON port_claims(owner_kind,owner_id);
CREATE INDEX tunnel_objects_owner ON tunnel_objects(owner_kind,owner_id);
CREATE UNIQUE INDEX node_identity_recovery_open ON node_identity_recoveries(node_id) WHERE status='issued';
CREATE INDEX node_identity_recovery_redeploy ON node_identity_recoveries(redeploy_state) WHERE redeploy_state IN ('pending','queued');
CREATE INDEX certificates_status ON certificates(status);
CREATE INDEX inbounds_certificate ON inbounds(certificate_id);
CREATE INDEX notifications_at ON notifications(at);
CREATE INDEX service_set_sources_service ON service_set_sources(service_id, position);
CREATE INDEX node_group_relays_rule ON node_group_relays(rule_id);
CREATE INDEX tunnel_rules_tunnel ON tunnel_rules(tunnel_id);
CREATE INDEX tunnel_rules_target ON tunnel_rules(target_inbound_id);
CREATE INDEX tunnel_rules_import_target ON tunnel_rules(target_import_id);
CREATE VIEW node_traffic_samples AS
 SELECT node_id,core,epoch,observed_at,upload_bytes,download_bytes FROM core_traffic_samples
 UNION ALL
 SELECT a.node_id,'' AS core,a.epoch,a.observed_at,a.upload_bytes,a.download_bytes FROM account_traffic_samples a
 WHERE NOT EXISTS(SELECT 1 FROM node_traffic_cursors n WHERE n.node_id=a.node_id AND n.epoch=a.epoch||'.node');
CREATE TRIGGER probe_targets_scheduled_insert BEFORE INSERT ON probe_targets
WHEN NEW.scheduled=1 AND (SELECT count(*) FROM probe_targets WHERE scheduled=1)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 scheduled probe targets'); END;
CREATE TRIGGER probe_targets_scheduled_update BEFORE UPDATE OF scheduled ON probe_targets
WHEN NEW.scheduled=1 AND OLD.scheduled=0 AND (SELECT count(*) FROM probe_targets WHERE scheduled=1)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 scheduled probe targets'); END;
CREATE TRIGGER node_latency_targets_insert BEFORE INSERT ON node_latency_targets
WHEN (SELECT count(*) FROM node_latency_targets WHERE node_id=NEW.node_id)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 node latency targets'); END;
CREATE TRIGGER node_latency_targets_update BEFORE UPDATE OF node_id ON node_latency_targets
WHEN NEW.node_id<>OLD.node_id AND (SELECT count(*) FROM node_latency_targets WHERE node_id=NEW.node_id)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 node latency targets'); END;
CREATE TRIGGER node_group_inbounds_insert AFTER INSERT ON node_group_inbounds BEGIN
 INSERT OR IGNORE INTO node_group_members SELECT NEW.group_id,node_id FROM inbounds WHERE id=NEW.inbound_id;
END;
CREATE TRIGGER node_group_inbounds_delete AFTER DELETE ON node_group_inbounds BEGIN
 DELETE FROM node_group_members WHERE group_id=OLD.group_id AND NOT EXISTS(SELECT 1 FROM node_group_servers s WHERE s.group_id=OLD.group_id AND s.node_id=node_group_members.node_id) AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=OLD.group_id AND i.node_id=node_group_members.node_id);
END;
CREATE TRIGGER node_group_inbound_move AFTER UPDATE OF node_id ON inbounds BEGIN
 DELETE FROM node_group_members WHERE NOT EXISTS(SELECT 1 FROM node_group_servers s WHERE s.group_id=node_group_members.group_id AND s.node_id=node_group_members.node_id) AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=node_group_members.group_id AND i.node_id=node_group_members.node_id);
 INSERT OR IGNORE INTO node_group_members SELECT group_id,NEW.node_id FROM node_group_inbounds WHERE inbound_id=NEW.id;
END;
CREATE TRIGGER node_group_servers_insert AFTER INSERT ON node_group_servers BEGIN
 INSERT OR IGNORE INTO node_group_members VALUES(NEW.group_id,NEW.node_id);
END;
CREATE TRIGGER node_group_servers_delete AFTER DELETE ON node_group_servers BEGIN
 DELETE FROM node_group_members WHERE group_id=OLD.group_id AND node_id=OLD.node_id AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=OLD.group_id AND i.node_id=OLD.node_id);
END;
CREATE TRIGGER node_group_servers_update AFTER UPDATE ON node_group_servers BEGIN
 DELETE FROM node_group_members WHERE group_id=OLD.group_id AND node_id=OLD.node_id AND NOT EXISTS(
 SELECT 1 FROM node_group_servers s WHERE s.group_id=OLD.group_id AND s.node_id=OLD.node_id) AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=OLD.group_id AND i.node_id=OLD.node_id);
 INSERT OR IGNORE INTO node_group_members VALUES(NEW.group_id,NEW.node_id);
END;
CREATE TRIGGER canvas_tunnel_inbound_deleted AFTER DELETE ON inbounds BEGIN DELETE FROM canvas_tunnel_objects WHERE object_kind='inbound' AND object_id=OLD.id; END;
CREATE TRIGGER canvas_tunnel_outbound_deleted AFTER DELETE ON outbounds BEGIN DELETE FROM canvas_tunnel_objects WHERE object_kind='outbound' AND object_id=OLD.id; END;
CREATE TRIGGER canvas_tunnel_chain_deleted AFTER DELETE ON proxy_chains BEGIN DELETE FROM canvas_tunnel_objects WHERE object_kind='chain' AND object_id=OLD.id; END;
CREATE TRIGGER canvas_entry_binding_detached AFTER UPDATE OF scheme_id ON canvas_entry_bindings
WHEN OLD.scheme_id IS NOT NULL AND NEW.scheme_id IS NULL
BEGIN
 UPDATE canvas_entry_bindings SET state='detached' WHERE id=NEW.id;
END;
CREATE TRIGGER tunnel_rule_node_address_changed AFTER UPDATE OF public_ip ON nodes
WHEN NEW.public_ip<>OLD.public_ip BEGIN
    UPDATE tunnel_rules SET status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
        draft_version=draft_version+1,
        updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
    WHERE target_inbound_id IN (SELECT id FROM inbounds WHERE node_id=NEW.id) AND status<>'pending_delete';
END;
CREATE TRIGGER tunnel_rule_inbound_port_changed AFTER UPDATE OF port ON inbounds
WHEN NEW.port<>OLD.port BEGIN
    UPDATE tunnel_rules SET status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
        draft_version=draft_version+1,
        updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
    WHERE target_inbound_id=NEW.id AND status<>'pending_delete';
END;
CREATE TRIGGER tunnel_rule_import_target_deleted AFTER UPDATE OF target_import_id ON tunnel_rules
WHEN OLD.target_import_id IS NOT NULL AND NEW.target_import_id IS NULL AND NEW.target_mode='import' BEGIN
    UPDATE tunnel_rules SET status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
        draft_version=draft_version+1,
        updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
    WHERE id=NEW.id AND status<>'pending_delete';
END;
