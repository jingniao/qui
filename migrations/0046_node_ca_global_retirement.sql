CREATE TABLE node_ca_global_retirement (
 id INTEGER PRIMARY KEY CHECK(id=1), plan_digest TEXT NOT NULL,
 record_digest TEXT NOT NULL, encrypted_record BLOB NOT NULL,
 readiness_json BLOB NOT NULL, phase TEXT NOT NULL CHECK(phase IN ('staged','retired')),
 staged_at TEXT NOT NULL, retired_at TEXT NOT NULL DEFAULT ''
);
