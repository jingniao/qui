CREATE TABLE recovery_budget_unknown_dispositions (
 id TEXT PRIMARY KEY,
 case_id TEXT NOT NULL,
 issue_id TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 previous_id TEXT NOT NULL,
 snapshot_digest TEXT NOT NULL CHECK(length(snapshot_digest)=64),
 issue_digest TEXT NOT NULL CHECK(length(issue_digest)=64),
 evidence_sha256 TEXT NOT NULL CHECK(length(evidence_sha256)=64),
 node_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 epoch TEXT NOT NULL CHECK(length(CAST(epoch AS BLOB)) BETWEEN 1 AND 128),
 disposition TEXT NOT NULL CHECK(disposition IN ('supplementable','continuable','retained_risk')),
 reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB))<=1024),
 owner TEXT NOT NULL,
 verified_at TEXT NOT NULL,
 created_at TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 FOREIGN KEY(case_id,issue_id) REFERENCES recovery_case_issues(case_id,issue_id) ON DELETE RESTRICT,
 UNIQUE(owner,request_key)
);
CREATE INDEX recovery_budget_unknown_lookup ON recovery_budget_unknown_dispositions(node_id,epoch,sequence);
