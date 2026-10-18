CREATE TABLE node_identity_recovery_decisions (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 sequence INTEGER NOT NULL CHECK(sequence>0),
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 owner TEXT NOT NULL,
 route TEXT NOT NULL CHECK(route IN ('restore_original','reconcile_trust','reenroll_review','record_unrecoverable_gap')),
 material_claim TEXT NOT NULL CHECK(material_claim IN ('original_available','key_unavailable','trust_unavailable','all_unavailable','not_verified')),
 identity_snapshot_digest TEXT NOT NULL CHECK(length(identity_snapshot_digest)=64),
 certificate_fingerprint TEXT NOT NULL,
 pending_fingerprint TEXT NOT NULL,
 plan_digest TEXT NOT NULL,
 signing_ca_fingerprint TEXT NOT NULL CHECK(length(signing_ca_fingerprint)=64),
 node_enabled INTEGER NOT NULL CHECK(node_enabled IN (0,1)),
 revoked_at TEXT NOT NULL,
 evidence_digests_json TEXT NOT NULL CHECK(length(evidence_digests_json)<=2048),
 status TEXT NOT NULL CHECK(status='recorded_requires_evidence'),
 created_at TEXT NOT NULL,
 UNIQUE(node_id,sequence),
 UNIQUE(node_id,request_key)
);
CREATE INDEX node_identity_recovery_decisions_node ON node_identity_recovery_decisions(node_id,sequence DESC);