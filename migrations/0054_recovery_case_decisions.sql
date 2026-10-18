CREATE TABLE recovery_case_decisions (
 id TEXT PRIMARY KEY,
 case_id TEXT NOT NULL,
 issue_id TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence>0),
 previous_id TEXT NOT NULL,
 snapshot_digest TEXT NOT NULL CHECK(length(snapshot_digest)=64),
 issue_digest TEXT NOT NULL CHECK(length(issue_digest)=64),
 evidence_sha256 TEXT NOT NULL,
 route TEXT NOT NULL CHECK(route IN ('collect_original_evidence','preserve_newer_node_state','review_original_transaction','record_unrecoverable_gap')),
 owner TEXT NOT NULL,
 verified_at TEXT NOT NULL,
 created_at TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 status TEXT NOT NULL CHECK(status='recorded_pending_execution'),
 FOREIGN KEY(case_id,issue_id) REFERENCES recovery_case_issues(case_id,issue_id) ON DELETE RESTRICT,
 UNIQUE(case_id,issue_id,sequence),
 UNIQUE(owner,request_key)
);
CREATE INDEX recovery_case_decisions_issue ON recovery_case_decisions(case_id,issue_id,sequence DESC);