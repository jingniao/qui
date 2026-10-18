CREATE TABLE recovery_connection_adoptions (
 decision_id TEXT PRIMARY KEY REFERENCES recovery_case_decisions(id) ON DELETE RESTRICT,
 case_id TEXT NOT NULL,
 issue_id TEXT NOT NULL,
 owner TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 FOREIGN KEY(case_id,issue_id) REFERENCES recovery_case_issues(case_id,issue_id) ON DELETE RESTRICT,
 UNIQUE(owner,request_key)
);
