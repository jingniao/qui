CREATE TABLE node_identity_replacements (
 id TEXT PRIMARY KEY,
 old_node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE RESTRICT,
 new_node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE RESTRICT,
 decision_id TEXT NOT NULL REFERENCES node_identity_recovery_decisions(id) ON DELETE RESTRICT,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 owner TEXT NOT NULL,
 old_snapshot_digest TEXT NOT NULL CHECK(length(old_snapshot_digest)=64),
 new_snapshot_digest TEXT NOT NULL CHECK(length(new_snapshot_digest)=64),
 old_fingerprint TEXT NOT NULL CHECK(length(old_fingerprint)=64),
 new_fingerprint TEXT NOT NULL CHECK(length(new_fingerprint)=64),
 created_at TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status='linked_state_review_pending'),
 CHECK(old_node_id<>new_node_id),
 CHECK(old_fingerprint<>new_fingerprint),
 UNIQUE(old_node_id,request_key)
);
