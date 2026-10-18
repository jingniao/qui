CREATE TABLE node_enrollment_receipts (
 token_hash TEXT PRIMARY KEY CHECK(length(token_hash)=64),
 node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE RESTRICT,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 public_key_digest TEXT NOT NULL CHECK(length(public_key_digest)=64),
 hostname TEXT NOT NULL CHECK(length(hostname)<=128),
 operating_system TEXT NOT NULL CHECK(length(operating_system)<=256),
 agent_version TEXT NOT NULL CHECK(length(agent_version)<=64),
 certificate_pem TEXT NOT NULL CHECK(length(certificate_pem)<=32768),
 certificate_fingerprint TEXT NOT NULL CHECK(length(certificate_fingerprint)=64),
 ca_pem TEXT NOT NULL CHECK(length(ca_pem)<=32768),
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
);
