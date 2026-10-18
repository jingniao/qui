CREATE TABLE node_ca_retirement_receipts (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 plan_digest TEXT NOT NULL,
 target_fingerprint TEXT NOT NULL,
 certificate_fingerprint TEXT NOT NULL,
 proof_digest TEXT NOT NULL,
 proof_json BLOB NOT NULL,
 first_confirmed_at TEXT NOT NULL,
 last_confirmed_at TEXT NOT NULL,
 PRIMARY KEY(node_id,plan_digest)
);
