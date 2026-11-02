-- GOST is the third core kind (docs/v1.3-design-gost.md 3.4). It terminates
-- forward inbounds only; the forward-only rule itself lives in the shared
-- application and DesiredState validation, not in a second SQL protocol rule.
-- Every CHECK below lists core names, so each one is widened to accept 'gost'
-- while keeping the existing rows, keys and indexes. A trigger cannot override a
-- failing CHECK, and 0125 must stay immutable.

ALTER TABLE nodes ADD COLUMN gost_core_supported INTEGER NOT NULL DEFAULT 0 CHECK(gost_core_supported IN (0,1));

-- inbounds is a heavily referenced parent (relay bindings, group members and K4
-- deletion triggers). Replace the column instead of the table: the old inline
-- CHECK disappears with its column, ids and foreign keys stay untouched.
ALTER TABLE inbounds ADD COLUMN core_gost TEXT NOT NULL DEFAULT '' CHECK(core_gost IN ('','xray','sing-box','gost'));
UPDATE inbounds SET core_gost=core;
ALTER TABLE inbounds DROP COLUMN core;
ALTER TABLE inbounds RENAME COLUMN core_gost TO core;

-- Leaf task tables: rebuild with the same columns and constraints plus 'gost',
-- copying every row by explicit column names. core_update_attempts, the only
-- table that ever referenced core_update_tasks, was dropped by 0107.
CREATE TABLE core_update_tasks_gost_staging (
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
INSERT INTO core_update_tasks_gost_staging(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding)
SELECT id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json,adding FROM core_update_tasks;
DROP TABLE core_update_tasks;
ALTER TABLE core_update_tasks_gost_staging RENAME TO core_update_tasks;
CREATE UNIQUE INDEX core_update_active_component ON core_update_tasks(node_id,component) WHERE status IN ('queued','running');

CREATE TABLE core_remove_tasks_gost_staging (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id),
 component TEXT NOT NULL CHECK(component IN ('xray','sing-box','gost')),
 revision INTEGER NOT NULL REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('queued','running','success','rejected','unconfirmed')),
 code TEXT NOT NULL DEFAULT '', result_json BLOB,
 created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
INSERT INTO core_remove_tasks_gost_staging(id,node_id,component,revision,status,code,result_json,created_by,created_at,updated_at)
SELECT id,node_id,component,revision,status,code,result_json,created_by,created_at,updated_at FROM core_remove_tasks;
DROP TABLE core_remove_tasks;
ALTER TABLE core_remove_tasks_gost_staging RENAME TO core_remove_tasks;
CREATE UNIQUE INDEX core_remove_active_component ON core_remove_tasks(node_id,component) WHERE status IN ('queued','running');

-- The deduplicating view reads both traffic tables, so it is dropped and
-- recreated unchanged around their replacement.
DROP VIEW node_traffic_samples;
CREATE TABLE node_traffic_cursors_gost_staging (
 node_id TEXT NOT NULL REFERENCES nodes(id), epoch TEXT NOT NULL, credential_id TEXT NOT NULL CHECK(credential_id='node'),
 core TEXT NOT NULL CHECK(core IN ('xray','sing-box','gost')), revision INTEGER NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), observed_at TEXT NOT NULL,
 PRIMARY KEY(node_id,epoch,credential_id)
);
INSERT INTO node_traffic_cursors_gost_staging(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at)
SELECT node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at FROM node_traffic_cursors;
CREATE TABLE core_traffic_samples_gost_staging (
 node_id TEXT NOT NULL REFERENCES nodes(id), core TEXT NOT NULL CHECK(core IN ('xray','sing-box','gost')), epoch TEXT NOT NULL, observed_at TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0),
 PRIMARY KEY(node_id,core,epoch,observed_at)
);
INSERT INTO core_traffic_samples_gost_staging(node_id,core,epoch,observed_at,upload_bytes,download_bytes)
SELECT node_id,core,epoch,observed_at,upload_bytes,download_bytes FROM core_traffic_samples;
DROP TABLE core_traffic_samples;
DROP TABLE node_traffic_cursors;
ALTER TABLE node_traffic_cursors_gost_staging RENAME TO node_traffic_cursors;
ALTER TABLE core_traffic_samples_gost_staging RENAME TO core_traffic_samples;
CREATE INDEX core_traffic_time ON core_traffic_samples(observed_at);
CREATE VIEW node_traffic_samples AS
 SELECT node_id,core,epoch,observed_at,upload_bytes,download_bytes FROM core_traffic_samples
 UNION ALL
 SELECT a.node_id,'' AS core,a.epoch,a.observed_at,a.upload_bytes,a.download_bytes FROM account_traffic_samples a
 WHERE NOT EXISTS(SELECT 1 FROM node_traffic_cursors n WHERE n.node_id=a.node_id AND n.epoch=a.epoch||'.node');
