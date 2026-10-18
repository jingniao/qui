CREATE TABLE quota_conversions (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 target_limit INTEGER NOT NULL CHECK(target_limit>0),
 status TEXT NOT NULL CHECK(status IN ('pausing','settling','reactivating','complete','blocked')),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX active_quota_conversion ON quota_conversions(user_id) WHERE status<>'complete';
CREATE TABLE quota_conversion_targets (
 conversion_id TEXT NOT NULL REFERENCES quota_conversions(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 previous_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 paused_revision INTEGER REFERENCES config_revisions(id),
 finite_revision INTEGER REFERENCES config_revisions(id),
 PRIMARY KEY(conversion_id,node_id)
);
