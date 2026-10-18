CREATE TABLE node_ca_transition (
 id INTEGER PRIMARY KEY CHECK(id=1),
 plan_digest TEXT NOT NULL,
 target_fingerprint TEXT NOT NULL,
 record_digest TEXT NOT NULL,
 encrypted_record BLOB NOT NULL,
 readiness_json BLOB NOT NULL,
 phase TEXT NOT NULL CHECK(phase IN ('staged','active')),
 staged_at TEXT NOT NULL,
 activated_at TEXT NOT NULL DEFAULT ''
);
