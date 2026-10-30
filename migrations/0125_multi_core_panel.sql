-- 0107 already backfilled component from the signed release catalogue.
ALTER TABLE core_update_tasks ADD COLUMN adding INTEGER NOT NULL DEFAULT 0 CHECK(adding IN (0,1));
DROP INDEX core_update_active_node;
CREATE UNIQUE INDEX core_update_active_component ON core_update_tasks(node_id,component) WHERE status IN ('queued','running');
ALTER TABLE inbounds ADD COLUMN core TEXT NOT NULL DEFAULT '' CHECK(core IN ('','xray','sing-box'));
DROP INDEX inbound_active_port;
ALTER TABLE nodes ADD COLUMN multi_core_supported INTEGER NOT NULL DEFAULT 0 CHECK(multi_core_supported IN (0,1));
ALTER TABLE nodes ADD COLUMN node_traffic_supported INTEGER NOT NULL DEFAULT 0 CHECK(node_traffic_supported IN (0,1));
ALTER TABLE node_health_checks ADD COLUMN core TEXT NOT NULL DEFAULT '';
ALTER TABLE node_restart_requests ADD COLUMN core TEXT NOT NULL DEFAULT '';
CREATE TABLE core_remove_tasks (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id),
 component TEXT NOT NULL CHECK(component IN ('xray','sing-box')),
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('queued','running','success','rejected','unconfirmed')),
 code TEXT NOT NULL DEFAULT '', result_json BLOB,
 created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX core_remove_active_component ON core_remove_tasks(node_id,component) WHERE status IN ('queued','running');
CREATE TABLE node_traffic_cursors (
 node_id TEXT NOT NULL REFERENCES nodes(id), epoch TEXT NOT NULL, credential_id TEXT NOT NULL CHECK(credential_id='node'),
 core TEXT NOT NULL CHECK(core IN ('xray','sing-box')), revision INTEGER NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), observed_at TEXT NOT NULL,
 PRIMARY KEY(node_id,epoch,credential_id)
);
CREATE TABLE core_traffic_samples (
 node_id TEXT NOT NULL REFERENCES nodes(id), core TEXT NOT NULL CHECK(core IN ('xray','sing-box')), epoch TEXT NOT NULL, observed_at TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0),
 PRIMARY KEY(node_id,core,epoch,observed_at)
);
CREATE INDEX core_traffic_time ON core_traffic_samples(observed_at);
CREATE VIEW node_traffic_samples AS
 SELECT node_id,core,epoch,observed_at,upload_bytes,download_bytes FROM core_traffic_samples
 UNION ALL
 SELECT a.node_id,'' AS core,a.epoch,a.observed_at,a.upload_bytes,a.download_bytes FROM account_traffic_samples a
 WHERE NOT EXISTS(SELECT 1 FROM node_traffic_cursors n WHERE n.node_id=a.node_id AND n.epoch=a.epoch||'.node');
