CREATE TABLE access_batches (
 node_id TEXT NOT NULL REFERENCES nodes(id), batch_id TEXT NOT NULL,
 digest TEXT NOT NULL, received_at TEXT NOT NULL,
 PRIMARY KEY(node_id,batch_id)
);
CREATE TABLE access_rollups (
 id TEXT PRIMARY KEY, bucket TEXT NOT NULL, node_id TEXT NOT NULL REFERENCES nodes(id),
 user_id TEXT NOT NULL DEFAULT '', device_id TEXT NOT NULL DEFAULT '',
 domain TEXT NOT NULL DEFAULT '', target_ip TEXT NOT NULL DEFAULT '',
 allowed INTEGER NOT NULL CHECK(allowed IN (0,1)), dimensions_json TEXT NOT NULL,
 connections INTEGER NOT NULL CHECK(connections>=0),
 upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0),
 traffic_known INTEGER NOT NULL CHECK(traffic_known IN (0,1))
);
CREATE INDEX access_time ON access_rollups(bucket DESC,id);
CREATE INDEX access_node_time ON access_rollups(node_id,bucket DESC);
CREATE INDEX access_user_time ON access_rollups(user_id,bucket DESC);
CREATE INDEX access_domain_time ON access_rollups(domain,bucket DESC);
CREATE INDEX access_blocked_time ON access_rollups(allowed,bucket DESC);
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
