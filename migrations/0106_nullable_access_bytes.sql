-- Phase 7B (plan D4): per-domain byte counts are nullable.
--
-- Only the removed fork cores measured exact bytes per domain. With upstream
-- cores an access rollup records the time bucket, user, node, domain or IP,
-- rule, action, configured outbound and connection count, while the byte columns
-- stay NULL whenever the measurement is unknown (traffic_known=0); the UI shows
-- "—". An unknown measurement is never stored as an invented zero.
--
-- SQLite cannot relax a NOT NULL constraint in place, so the table is rebuilt
-- with the same columns and indexes: existing rows keep their byte values, and a
-- CHECK still rejects a negative value. No retained table references
-- access_rollups, so the rename does not touch a foreign key.

ALTER TABLE access_rollups RENAME TO access_rollups_staging;
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
INSERT INTO access_rollups(id,bucket,node_id,user_id,device_id,domain,target_ip,allowed,dimensions_json,connections,upload_bytes,download_bytes,traffic_known)
 SELECT id,bucket,node_id,user_id,device_id,domain,target_ip,allowed,dimensions_json,connections,upload_bytes,download_bytes,traffic_known FROM access_rollups_staging;
DROP TABLE access_rollups_staging;
CREATE INDEX access_time ON access_rollups(bucket DESC,id);
CREATE INDEX access_node_time ON access_rollups(node_id,bucket DESC);
CREATE INDEX access_user_time ON access_rollups(user_id,bucket DESC);
CREATE INDEX access_domain_time ON access_rollups(domain,bucket DESC);
CREATE INDEX access_blocked_time ON access_rollups(allowed,bucket DESC);