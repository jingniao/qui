CREATE TABLE controller_isolation_approvals (
 id TEXT PRIMARY KEY,
 evidence_id TEXT NOT NULL,
 candidate_sha256 TEXT NOT NULL CHECK(length(candidate_sha256)=64),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 source_id TEXT NOT NULL,
 target_plan_digest TEXT NOT NULL CHECK(length(target_plan_digest)=64),
 evidence_digest TEXT NOT NULL CHECK(length(evidence_digest)=64),
 approved_by TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 status TEXT NOT NULL CHECK(status='isolation_approved'),
 created_at TEXT NOT NULL,
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 UNIQUE(request_key)
);
