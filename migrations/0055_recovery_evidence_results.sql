CREATE TABLE recovery_evidence_results (
 id TEXT PRIMARY KEY,
 case_id TEXT NOT NULL,
 issue_id TEXT NOT NULL,
 decision_id TEXT NOT NULL UNIQUE REFERENCES recovery_case_decisions(id) ON DELETE RESTRICT,
 followup_case_id TEXT NOT NULL REFERENCES recovery_cases(id) ON DELETE RESTRICT,
 original_snapshot TEXT NOT NULL CHECK(length(original_snapshot)=64),
 followup_snapshot TEXT NOT NULL CHECK(length(followup_snapshot)=64),
 evidence_sha256 TEXT NOT NULL CHECK(length(evidence_sha256)=64),
 observed_at TEXT NOT NULL,
 relation TEXT NOT NULL CHECK(relation IN ('issue_still_observed','issue_not_observed_requires_review')),
 owner TEXT NOT NULL,
 verified_at TEXT NOT NULL,
 created_at TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 FOREIGN KEY(case_id,issue_id) REFERENCES recovery_case_issues(case_id,issue_id) ON DELETE RESTRICT,
 UNIQUE(owner,request_key)
);
CREATE INDEX recovery_evidence_results_issue ON recovery_evidence_results(case_id,issue_id,created_at DESC,id DESC);