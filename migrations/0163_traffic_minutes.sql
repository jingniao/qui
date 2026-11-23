-- Exact UTC-minute display storage for samples older than 24 hours.
-- Raw samples remain authoritative for ingestion and accounting.
CREATE TABLE core_traffic_minutes (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 core TEXT NOT NULL CHECK(core IN ('xray','sing-box','gost')),
 epoch TEXT NOT NULL,
 bucket TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(typeof(upload_bytes)='integer' AND upload_bytes>=0),
 download_bytes INTEGER NOT NULL CHECK(typeof(download_bytes)='integer' AND download_bytes>=0),
 samples INTEGER NOT NULL CHECK(typeof(samples)='integer' AND samples>0),
 PRIMARY KEY(node_id,core,epoch,bucket)
);
CREATE INDEX core_traffic_minutes_time ON core_traffic_minutes(bucket,node_id);
CREATE TABLE account_traffic_minutes (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 epoch TEXT NOT NULL,
 credential_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 device_id TEXT NOT NULL,
 bucket TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL CHECK(typeof(upload_bytes)='integer' AND upload_bytes>=0),
 download_bytes INTEGER NOT NULL CHECK(typeof(download_bytes)='integer' AND download_bytes>=0),
 samples INTEGER NOT NULL CHECK(typeof(samples)='integer' AND samples>0),
 PRIMARY KEY(node_id,epoch,credential_id,user_id,device_id,bucket)
);
CREATE INDEX account_traffic_minutes_time ON account_traffic_minutes(bucket,node_id,user_id,device_id);

-- Minute writes replace raw rows for a display hour, so the derived hour index
-- must be rebuilt before it is used again.
CREATE TRIGGER core_traffic_minute_insert AFTER INSERT ON core_traffic_minutes
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('core',NEW.node_id,NEW.epoch,substr(NEW.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
END;
CREATE TRIGGER core_traffic_minute_update AFTER UPDATE ON core_traffic_minutes
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('core',OLD.node_id,OLD.epoch,substr(OLD.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('core',NEW.node_id,NEW.epoch,substr(NEW.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
END;
CREATE TRIGGER core_traffic_minute_delete AFTER DELETE ON core_traffic_minutes
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('core',OLD.node_id,OLD.epoch,substr(OLD.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
END;
CREATE TRIGGER account_traffic_minute_insert AFTER INSERT ON account_traffic_minutes
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('account',NEW.node_id,NEW.epoch,substr(NEW.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
END;
CREATE TRIGGER account_traffic_minute_update AFTER UPDATE ON account_traffic_minutes
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('account',OLD.node_id,OLD.epoch,substr(OLD.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('account',NEW.node_id,NEW.epoch,substr(NEW.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
END;
CREATE TRIGGER account_traffic_minute_delete AFTER DELETE ON account_traffic_minutes
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 VALUES('account',OLD.node_id,OLD.epoch,substr(OLD.bucket,1,13)||':00:00Z',0)
 ON CONFLICT(source,node_id,epoch,bucket) DO UPDATE SET ready=0;
END;
