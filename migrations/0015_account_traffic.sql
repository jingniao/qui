CREATE TABLE traffic_batches(node_id TEXT NOT NULL REFERENCES nodes(id),batch_id TEXT NOT NULL,digest TEXT NOT NULL,received_at TEXT NOT NULL,PRIMARY KEY(node_id,batch_id));
CREATE TABLE traffic_cursors(node_id TEXT NOT NULL,epoch TEXT NOT NULL,credential_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,observed_at TEXT NOT NULL,PRIMARY KEY(node_id,epoch,credential_id));
CREATE TABLE account_traffic_hourly(bucket TEXT NOT NULL,node_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(bucket,node_id,user_id,device_id));
CREATE TABLE account_traffic_daily(bucket TEXT NOT NULL,node_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(bucket,node_id,user_id,device_id));
CREATE TABLE account_traffic_samples(node_id TEXT NOT NULL,epoch TEXT NOT NULL,credential_id TEXT NOT NULL,observed_at TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(node_id,epoch,credential_id,observed_at));
CREATE INDEX account_traffic_time ON account_traffic_samples(observed_at);
-- Owned and updated only by the user module's repository.
CREATE TABLE user_traffic_cursors(node_id TEXT NOT NULL,epoch TEXT NOT NULL,credential_id TEXT NOT NULL,user_id TEXT NOT NULL,device_id TEXT NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,PRIMARY KEY(node_id,epoch,credential_id));
