ALTER TABLE node_enrollment_tokens ADD COLUMN purpose TEXT NOT NULL DEFAULT 'enroll' CHECK(purpose IN ('enroll','recover'));
ALTER TABLE node_enrollment_tokens ADD COLUMN bound_fingerprint TEXT NOT NULL DEFAULT '';

CREATE TABLE node_identity_recoveries (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 token_hash TEXT NOT NULL UNIQUE CHECK(length(token_hash)=64),
 previous_fingerprint TEXT NOT NULL CHECK(length(previous_fingerprint)=64),
 new_fingerprint TEXT NOT NULL DEFAULT '' CHECK(new_fingerprint='' OR length(new_fingerprint)=64),
 owner TEXT NOT NULL,
 revoked_on_issue INTEGER NOT NULL CHECK(revoked_on_issue IN (0,1)),
 status TEXT NOT NULL CHECK(status IN ('issued','consumed','superseded','expired')),
 redeploy_state TEXT NOT NULL DEFAULT 'none' CHECK(redeploy_state IN ('none','pending','queued','success','failed','not_needed')),
 redeploy_revision INTEGER NOT NULL DEFAULT 0,
 redeploy_code TEXT NOT NULL DEFAULT '' CHECK(length(redeploy_code)<=64),
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 consumed_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX node_identity_recovery_open ON node_identity_recoveries(node_id) WHERE status='issued';
CREATE INDEX node_identity_recovery_redeploy ON node_identity_recoveries(redeploy_state) WHERE redeploy_state IN ('pending','queued');

CREATE TABLE node_identity_retirements (
 fingerprint TEXT PRIMARY KEY CHECK(length(fingerprint)=64),
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 recovery_id TEXT NOT NULL,
 retired_at TEXT NOT NULL
);
