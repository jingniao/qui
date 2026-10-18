CREATE TABLE node_ca_successor_rounds (
 round_number INTEGER PRIMARY KEY CHECK(round_number>=2),
 plan_digest TEXT NOT NULL UNIQUE CHECK(length(plan_digest)=64),
 parent_plan_digest TEXT NOT NULL UNIQUE CHECK(length(parent_plan_digest)=64),
 parent_active_digest TEXT NOT NULL CHECK(length(parent_active_digest)=64),
 parent_retirement_digest TEXT NOT NULL CHECK(length(parent_retirement_digest)=64),
 source_fingerprint TEXT NOT NULL CHECK(length(source_fingerprint)=64),
 target_fingerprint TEXT NOT NULL CHECK(length(target_fingerprint)=64),
 candidate_digest TEXT NOT NULL CHECK(length(candidate_digest)=64),
 encrypted_candidate BLOB NOT NULL CHECK(length(encrypted_candidate)>0 AND length(encrypted_candidate)<=32768),
 readiness_json BLOB NOT NULL CHECK(length(readiness_json)>0 AND length(readiness_json)<=1048576),
 phase TEXT NOT NULL CHECK(phase='staged'),
 staged_at TEXT NOT NULL
);