CREATE TABLE owner_expired_certificate_renewals (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
	request_json TEXT NOT NULL CHECK(length(request_json)<=32768),
 decision_id TEXT NOT NULL REFERENCES node_identity_recovery_decisions(id) ON DELETE RESTRICT,
 owner TEXT NOT NULL,
 original_snapshot_digest TEXT NOT NULL CHECK(length(original_snapshot_digest)=64),
 previous_fingerprint TEXT NOT NULL CHECK(length(previous_fingerprint)=64),
 signing_ca_fingerprint TEXT NOT NULL CHECK(length(signing_ca_fingerprint)=64),
 certificate_pem TEXT NOT NULL,
 certificate_fingerprint TEXT NOT NULL CHECK(length(certificate_fingerprint)=64),
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(node_id,request_key)
);
