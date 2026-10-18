-- Phase 10: remove the recovery-case machinery.
--
-- Dropped here: the node identity recovery decisions, identity replacement,
-- identity isolation, loss state review and loss disposition tables, and the
-- recovery-case completion/adoption/settlement, controller handoff and
-- controller isolation tables. Node lifecycle is now enroll -> active ->
-- disabled; a lost or compromised node is replaced by disabling it (which
-- revokes its certificate) and enrolling a new one.
--
-- Kept because retained code still reads them:
--   recovery_cases, recovery_case_issues, recovery_case_decisions,
--   recovery_identity_completions   -> system/ca_successor_trust.go and
--                                      backup/recovery_identity_completions.go
--   owner_expired_certificate_renewals -> backup/owner_renewal_issuance.go,
--                                      backup/recovered_round_parents.go
--   recovered_budget_retirements     -> user budget/return accounting (phase 4B)
--   recovery_owner_credentials       -> auth owner credential recovery
--   node_accepted_messages, node_enrollment_receipts -> normal node operation

-- owner_expired_certificate_renewals is retained for the successor-CA trust
-- guard, but its original foreign key to node_identity_recovery_decisions must
-- go before that table can be dropped. Rebuild it without that key.
CREATE TABLE owner_expired_certificate_renewals_0100 (
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 request_json TEXT NOT NULL CHECK(length(request_json)<=32768),
 decision_id TEXT NOT NULL,
 owner TEXT NOT NULL,
 original_snapshot_digest TEXT NOT NULL CHECK(length(original_snapshot_digest)=64),
 previous_fingerprint TEXT NOT NULL CHECK(length(previous_fingerprint)=64),
 signing_ca_fingerprint TEXT NOT NULL CHECK(length(signing_ca_fingerprint)=64),
 certificate_pem TEXT NOT NULL,
 certificate_fingerprint TEXT NOT NULL CHECK(length(certificate_fingerprint)=64),
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL,
 previous_certificate_pem TEXT NOT NULL DEFAULT '',
 previous_expires_at TEXT NOT NULL DEFAULT '',
 signing_ca_pem TEXT NOT NULL DEFAULT '',
 issuance_proof_json TEXT NOT NULL DEFAULT '',
 previous_ca_pem TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(node_id,request_key)
);
INSERT INTO owner_expired_certificate_renewals_0100
 SELECT node_id,request_key,request_digest,request_json,decision_id,owner,original_snapshot_digest,previous_fingerprint,signing_ca_fingerprint,certificate_pem,certificate_fingerprint,expires_at,created_at,previous_certificate_pem,previous_expires_at,signing_ca_pem,issuance_proof_json,previous_ca_pem
 FROM owner_expired_certificate_renewals;
DROP TABLE owner_expired_certificate_renewals;
ALTER TABLE owner_expired_certificate_renewals_0100 RENAME TO owner_expired_certificate_renewals;

DROP TABLE IF EXISTS recovery_evidence_results;
DROP TABLE IF EXISTS recovery_accounting_checks;
DROP TABLE IF EXISTS recovery_traffic_executions;
DROP TABLE IF EXISTS recovery_revision_applications;
DROP TABLE IF EXISTS recovery_message_completions;
DROP TABLE IF EXISTS recovery_update_completions;
DROP TABLE IF EXISTS recovery_agent_result_completions;
DROP TABLE IF EXISTS recovery_core_result_completions;
DROP TABLE IF EXISTS recovery_final_settlements;
DROP TABLE IF EXISTS recovery_connection_completions;
DROP TABLE IF EXISTS recovery_connection_adoptions;
DROP TABLE IF EXISTS recovery_budget_adoptions;
DROP TABLE IF EXISTS recovery_budget_unknown_dispositions;
DROP TABLE IF EXISTS controller_handoff_completions;
DROP TABLE IF EXISTS controller_isolation_evidence;
DROP TABLE IF EXISTS controller_isolation_approvals;
DROP TABLE IF EXISTS node_loss_dispositions;
DROP TABLE IF EXISTS node_identity_replacements;
DROP TABLE IF EXISTS node_loss_state_reviews;
DROP TABLE IF EXISTS node_identity_isolations;
DROP TABLE IF EXISTS node_identity_recovery_decisions;
