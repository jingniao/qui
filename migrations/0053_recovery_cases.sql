CREATE TABLE recovery_cases (
 id TEXT PRIMARY KEY,
 source_id TEXT NOT NULL,
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 ledger_sha256 TEXT NOT NULL CHECK(length(ledger_sha256)=64),
 snapshot_digest TEXT NOT NULL CHECK(length(snapshot_digest)=64),
 issue_count INTEGER NOT NULL CHECK(issue_count>=0 AND issue_count<=10000),
 verified_at TEXT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 status TEXT NOT NULL CHECK(status='evidence_registered_review_pending'),
 UNIQUE(created_by,request_key),
 UNIQUE(source_id,plan_sha256,ledger_sha256)
);
CREATE TABLE recovery_case_issues (
 case_id TEXT NOT NULL REFERENCES recovery_cases(id) ON DELETE RESTRICT,
 issue_id TEXT NOT NULL CHECK(length(issue_id)=64),
 node_id TEXT NOT NULL,
 category TEXT NOT NULL,
 evidence_sha256 TEXT NOT NULL,
 payload_json TEXT NOT NULL CHECK(length(CAST(payload_json AS BLOB))<=16384),
 PRIMARY KEY(case_id,issue_id)
);
CREATE INDEX recovery_cases_created ON recovery_cases(created_at DESC,id DESC);