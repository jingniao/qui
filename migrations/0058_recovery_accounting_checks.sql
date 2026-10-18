CREATE TABLE recovery_accounting_checks (
 id TEXT PRIMARY KEY,
 case_id TEXT NOT NULL REFERENCES recovery_cases(id) ON DELETE RESTRICT,
 node_id TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence BETWEEN 1 AND 2147483647),
 previous_id TEXT NOT NULL,
 previous_digest TEXT NOT NULL,
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 result_digest TEXT NOT NULL CHECK(length(result_digest)=64),
 owner TEXT NOT NULL,
 created_at TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 UNIQUE(case_id,node_id,sequence),
 UNIQUE(owner,request_key)
);
CREATE INDEX recovery_accounting_checks_page ON recovery_accounting_checks(case_id,created_at DESC,id DESC);
