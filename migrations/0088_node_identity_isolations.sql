CREATE TABLE node_identity_isolations (
 id TEXT PRIMARY KEY,
 node_id TEXT NOT NULL UNIQUE,
 decision_id TEXT NOT NULL,
 owner TEXT NOT NULL,
 identity_snapshot_digest TEXT NOT NULL CHECK(length(identity_snapshot_digest)=64),
 certificate_fingerprint TEXT NOT NULL CHECK(length(certificate_fingerprint)=64),
 prior_revoked_at TEXT NOT NULL,
 revoked_at TEXT NOT NULL,
 material_claim TEXT NOT NULL CHECK(material_claim IN ('key_unavailable','all_unavailable')),
 status TEXT NOT NULL CHECK(status='identity_isolated_pending_replacement'),
 created_at TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 UNIQUE(owner,request_key)
);
