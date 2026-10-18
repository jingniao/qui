CREATE TABLE recovery_final_settlements (
 case_id TEXT NOT NULL REFERENCES recovery_cases(id) ON DELETE RESTRICT,
 decision_id TEXT PRIMARY KEY REFERENCES recovery_case_decisions(id) ON DELETE RESTRICT,
 owner TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 UNIQUE(owner,request_key)
);
