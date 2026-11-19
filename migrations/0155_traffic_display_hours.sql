-- Rebuildable read index; the source samples and historical core hours remain
-- authoritative. Existing buckets are prepared in small background transactions.
CREATE TABLE traffic_sample_hours (
 source TEXT NOT NULL CHECK(source IN ('core','account')),
 node_id TEXT NOT NULL,
 epoch TEXT NOT NULL,
 bucket TEXT NOT NULL,
 upload_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(upload_bytes)='integer' AND upload_bytes>=0),
 download_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(download_bytes)='integer' AND download_bytes>=0),
 samples INTEGER NOT NULL DEFAULT 0 CHECK(typeof(samples)='integer' AND samples>=0),
 ready INTEGER NOT NULL CHECK(ready IN (0,1)),
 overflow INTEGER NOT NULL DEFAULT 0 CHECK(overflow IN (0,1)),
 PRIMARY KEY(source,node_id,epoch,bucket)
);
CREATE INDEX traffic_sample_hours_time ON traffic_sample_hours(bucket,node_id);
CREATE INDEX traffic_sample_hours_pending ON traffic_sample_hours(ready,source,node_id,epoch,bucket);
CREATE INDEX core_traffic_sample_hour ON core_traffic_samples(node_id,epoch,substr(observed_at,1,13));
CREATE INDEX account_traffic_sample_hour ON account_traffic_samples(node_id,epoch,substr(observed_at,1,13));
INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 SELECT DISTINCT 'core',node_id,epoch,substr(observed_at,1,13)||':00:00Z',0 FROM core_traffic_samples;
INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready)
 SELECT DISTINCT 'account',node_id,epoch,substr(observed_at,1,13)||':00:00Z',0 FROM account_traffic_samples;

CREATE TRIGGER core_traffic_hour_insert AFTER INSERT ON core_traffic_samples
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready) VALUES('core',NEW.node_id,NEW.epoch,substr(NEW.observed_at,1,13)||':00:00Z',1) ON CONFLICT DO NOTHING;
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN upload_bytes+NEW.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN download_bytes+NEW.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN samples+1 ELSE 0 END,
 overflow=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN 0 ELSE 1 END
 WHERE source='core' AND node_id=NEW.node_id AND epoch=NEW.epoch AND bucket=substr(NEW.observed_at,1,13)||':00:00Z' AND ready=1;
END;
CREATE TRIGGER core_traffic_hour_update AFTER UPDATE ON core_traffic_samples
BEGIN
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 THEN upload_bytes-OLD.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 THEN download_bytes-OLD.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 THEN samples-1 ELSE 0 END,
 ready=CASE WHEN overflow=0 THEN 1 ELSE 0 END,overflow=0
 WHERE source='core' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1;
 DELETE FROM traffic_sample_hours WHERE source='core' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1 AND samples=0;
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready) VALUES('core',NEW.node_id,NEW.epoch,substr(NEW.observed_at,1,13)||':00:00Z',1) ON CONFLICT DO NOTHING;
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN upload_bytes+NEW.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN download_bytes+NEW.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN samples+1 ELSE 0 END,
 overflow=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN 0 ELSE 1 END
 WHERE source='core' AND node_id=NEW.node_id AND epoch=NEW.epoch AND bucket=substr(NEW.observed_at,1,13)||':00:00Z' AND ready=1;
END;
CREATE TRIGGER core_traffic_hour_delete AFTER DELETE ON core_traffic_samples
BEGIN
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 THEN upload_bytes-OLD.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 THEN download_bytes-OLD.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 THEN samples-1 ELSE 0 END,
 ready=CASE WHEN overflow=0 THEN 1 ELSE 0 END,overflow=0
 WHERE source='core' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1;
 DELETE FROM traffic_sample_hours WHERE source='core' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1 AND samples=0;
END;

CREATE TRIGGER account_traffic_hour_insert AFTER INSERT ON account_traffic_samples
BEGIN
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready) VALUES('account',NEW.node_id,NEW.epoch,substr(NEW.observed_at,1,13)||':00:00Z',1) ON CONFLICT DO NOTHING;
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN upload_bytes+NEW.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN download_bytes+NEW.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN samples+1 ELSE 0 END,
 overflow=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN 0 ELSE 1 END
 WHERE source='account' AND node_id=NEW.node_id AND epoch=NEW.epoch AND bucket=substr(NEW.observed_at,1,13)||':00:00Z' AND ready=1;
END;
CREATE TRIGGER account_traffic_hour_update AFTER UPDATE ON account_traffic_samples
BEGIN
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 THEN upload_bytes-OLD.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 THEN download_bytes-OLD.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 THEN samples-1 ELSE 0 END,
 ready=CASE WHEN overflow=0 THEN 1 ELSE 0 END,overflow=0
 WHERE source='account' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1;
 DELETE FROM traffic_sample_hours WHERE source='account' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1 AND samples=0;
 INSERT INTO traffic_sample_hours(source,node_id,epoch,bucket,ready) VALUES('account',NEW.node_id,NEW.epoch,substr(NEW.observed_at,1,13)||':00:00Z',1) ON CONFLICT DO NOTHING;
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN upload_bytes+NEW.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN download_bytes+NEW.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN samples+1 ELSE 0 END,
 overflow=CASE WHEN overflow=0 AND upload_bytes<=9223372036854775807-NEW.upload_bytes AND download_bytes<=9223372036854775807-NEW.download_bytes AND samples<9223372036854775807 THEN 0 ELSE 1 END
 WHERE source='account' AND node_id=NEW.node_id AND epoch=NEW.epoch AND bucket=substr(NEW.observed_at,1,13)||':00:00Z' AND ready=1;
END;
CREATE TRIGGER account_traffic_hour_delete AFTER DELETE ON account_traffic_samples
BEGIN
 UPDATE traffic_sample_hours SET
 upload_bytes=CASE WHEN overflow=0 THEN upload_bytes-OLD.upload_bytes ELSE 0 END,
 download_bytes=CASE WHEN overflow=0 THEN download_bytes-OLD.download_bytes ELSE 0 END,
 samples=CASE WHEN overflow=0 THEN samples-1 ELSE 0 END,
 ready=CASE WHEN overflow=0 THEN 1 ELSE 0 END,overflow=0
 WHERE source='account' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1;
 DELETE FROM traffic_sample_hours WHERE source='account' AND node_id=OLD.node_id AND epoch=OLD.epoch AND bucket=substr(OLD.observed_at,1,13)||':00:00Z' AND ready=1 AND samples=0;
END;
