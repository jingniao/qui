CREATE TABLE controller_isolation_evidence (
 id TEXT PRIMARY KEY,
 candidate_sha256 TEXT NOT NULL CHECK(length(candidate_sha256)=64),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 source_id TEXT NOT NULL,
 target_plan_digest TEXT NOT NULL CHECK(length(target_plan_digest)=64),
 evidence_kind TEXT NOT NULL CHECK(evidence_kind IN ('power_off_attestation','host_isolated_attestation','network_isolated_attestation','hypervisor_snapshot_attestation')),
 evidence_digest TEXT NOT NULL CHECK(length(evidence_digest)=64),
 requested_by TEXT NOT NULL,
 approved_by TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 local_lock_sufficient INTEGER NOT NULL CHECK(local_lock_sufficient=0),
 status TEXT NOT NULL CHECK(status='external_isolation_evidence_recorded'),
 created_at TEXT NOT NULL,
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 UNIQUE(requested_by,request_key)
);
