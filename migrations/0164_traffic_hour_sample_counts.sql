-- Older hour preparation counted each minute row as one sample. Invalidate
-- only derived hours containing minute rows, once; the bounded background
-- preparation rebuilds them from unchanged authoritative sources.
UPDATE traffic_sample_hours SET ready=0
WHERE ready=1 AND (source,node_id,epoch,bucket) IN (
 SELECT 'core',node_id,epoch,substr(bucket,1,13)||':00:00Z' FROM core_traffic_minutes
 UNION
 SELECT 'account',node_id,epoch,substr(bucket,1,13)||':00:00Z' FROM account_traffic_minutes
);
