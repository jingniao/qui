-- Direct-URL core updates: the Owner supplies version, official asset URL and
-- archive SHA-256. The signed release catalogue stays for Agent self-update
-- (Phase 12), so historical core tasks keep a reference to it.
DROP TABLE IF EXISTS core_update_attempts;

CREATE TABLE core_update_tasks_staging (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id),
  baseline_revision INTEGER NOT NULL REFERENCES config_revisions(id),
  release_id TEXT REFERENCES release_catalogue(id),
  component TEXT NOT NULL CHECK(component IN ('xray','sing-box')),
  version TEXT NOT NULL,
  url TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64),
  sequence INTEGER NOT NULL CHECK(sequence>0),
  status TEXT NOT NULL CHECK(status IN ('queued','running','success','rolled_back','rejected','failed_closed','cleared')),
  command_id TEXT NOT NULL UNIQUE,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  result_json BLOB
);

-- Historical rows keep their catalogue binding, but the direct-URL columns are
-- backfilled empty because the signed archive digest is not a binary digest.
INSERT INTO core_update_tasks_staging(id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json)
SELECT t.id,t.node_id,t.baseline_revision,t.release_id,coalesce(c.component,'xray'),coalesce(c.version,'0.0.0'),'',coalesce(c.digest,printf('%064d',0)),coalesce(c.release_sequence,1),
       CASE WHEN t.status='failed_closed' THEN 'cleared' ELSE t.status END,
       t.command_id,t.created_by,t.created_at,t.updated_at,NULL
FROM core_update_tasks t
LEFT JOIN release_catalogue c ON c.id=t.release_id;

DROP TABLE core_update_tasks;
ALTER TABLE core_update_tasks_staging RENAME TO core_update_tasks;
CREATE UNIQUE INDEX core_update_active_node ON core_update_tasks(node_id) WHERE status IN ('queued','running');
