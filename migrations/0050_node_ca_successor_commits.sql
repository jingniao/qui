ALTER TABLE node_ca_successor_rounds RENAME TO node_ca_successor_rounds_staging;
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
 phase TEXT NOT NULL CHECK(phase IN ('staged','installing','active','retiring','retired')),
 staged_at TEXT NOT NULL,
 head_digest TEXT NOT NULL DEFAULT '',
 encrypted_head BLOB NOT NULL DEFAULT X'',
 activation_readiness BLOB NOT NULL DEFAULT X'',
 activated_at TEXT NOT NULL DEFAULT '',
 retirement_digest TEXT NOT NULL DEFAULT '',
 encrypted_retirement BLOB NOT NULL DEFAULT X'',
 retired_at TEXT NOT NULL DEFAULT '',
 CHECK((phase='staged' AND head_digest='' AND length(encrypted_head)=0) OR (phase<>'staged' AND length(head_digest)=64 AND length(encrypted_head)>0 AND length(encrypted_head)<=1048576)),
 CHECK(phase NOT IN ('retiring','retired') OR (length(retirement_digest)=64 AND length(encrypted_retirement)>0 AND length(encrypted_retirement)<=32768))
);
INSERT INTO node_ca_successor_rounds(round_number,plan_digest,parent_plan_digest,parent_active_digest,parent_retirement_digest,source_fingerprint,target_fingerprint,candidate_digest,encrypted_candidate,readiness_json,phase,staged_at)
 SELECT round_number,plan_digest,parent_plan_digest,parent_active_digest,parent_retirement_digest,source_fingerprint,target_fingerprint,candidate_digest,encrypted_candidate,readiness_json,phase,staged_at FROM node_ca_successor_rounds_staging;
DROP TABLE node_ca_successor_rounds_staging;
CREATE UNIQUE INDEX node_ca_one_unfinished_round ON node_ca_successor_rounds((1)) WHERE phase<>'retired';