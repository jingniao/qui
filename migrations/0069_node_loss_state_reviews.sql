CREATE TABLE node_loss_state_reviews (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 decision_id TEXT NOT NULL REFERENCES node_identity_recovery_decisions(id) ON DELETE RESTRICT,
 sequence INTEGER NOT NULL CHECK(sequence>0),
 scope TEXT NOT NULL CHECK(scope IN ('identity_and_private_key','approved_trust_and_ca_rounds','unconfirmed_consumption','outstanding_byte_allowances','connection_allowances_and_retirements','message_ack_and_pending_commands','applied_revision_and_accounting_sources','accepted_updates_and_installation')),
 finding TEXT NOT NULL CHECK(finding IN ('pending_verification','original_source_referenced','declared_unrecoverable')),
 previous_scope_id TEXT NOT NULL,
 identity_snapshot_digest TEXT NOT NULL CHECK(length(identity_snapshot_digest)=64),
 evidence_digests_json TEXT NOT NULL CHECK(length(evidence_digests_json)<=2048),
 owner TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 status TEXT NOT NULL CHECK(status='recorded_review_pending'),
 created_at TEXT NOT NULL,
 UNIQUE(node_id,sequence),
 UNIQUE(node_id,request_key)
);
CREATE INDEX node_loss_state_reviews_scope ON node_loss_state_reviews(node_id,scope,sequence DESC);
