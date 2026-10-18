CREATE TABLE node_ca_trust_receipts (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 plan_digest TEXT NOT NULL,
 source_fingerprint TEXT NOT NULL,
 target_fingerprint TEXT NOT NULL,
 certificate_fingerprint TEXT NOT NULL,
 bridge_expires_at TEXT NOT NULL,
 first_confirmed_at TEXT NOT NULL,
 last_confirmed_at TEXT NOT NULL,
 manifest_json BLOB NOT NULL,
 source_ca BLOB NOT NULL,
 target_ca BLOB NOT NULL,
 bridge_ca BLOB NOT NULL,
 PRIMARY KEY(node_id,plan_digest)
);
