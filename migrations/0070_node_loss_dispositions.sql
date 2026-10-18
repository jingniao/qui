CREATE TABLE node_loss_dispositions (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE RESTRICT,
 new_node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 replacement_id TEXT NOT NULL REFERENCES node_identity_replacements(id) ON DELETE RESTRICT,
 decision_id TEXT NOT NULL REFERENCES node_identity_recovery_decisions(id) ON DELETE RESTRICT,
 old_snapshot_digest TEXT NOT NULL CHECK(length(old_snapshot_digest)=64),
 new_snapshot_digest TEXT NOT NULL CHECK(length(new_snapshot_digest)=64),
 scope_review_ids_json TEXT NOT NULL CHECK(length(scope_review_ids_json)<=2048),
 owner TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 status TEXT NOT NULL CHECK(status='unrecoverable_gaps_retained'),
 created_at TEXT NOT NULL,
 CHECK(node_id<>new_node_id),
 UNIQUE(node_id,request_key)
);
