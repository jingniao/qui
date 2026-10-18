-- Phase 11: one node CA.
--
-- Dropped here: the successor/recovered CA round, transition, trust,
-- retirement, recovered-root and CA-operation-request tables, and the
-- recovery-case tables that migration 0100 kept only for the CA trust guard
-- and offline owner-credential recovery. The node CA is now a single
-- ca.crt/ca.key pair: enrollment signs a leaf, renewal and key rotation
-- reissue it, and disabling a node revokes access. CA rotation is a manual
-- re-enrollment procedure.
--
-- Children are dropped before parents. No kept table references any dropped
-- table, so no table is rebuilt.

DROP TABLE IF EXISTS node_ca_successor_retirement_assessments;
DROP TABLE IF EXISTS node_ca_successor_rounds;
DROP TABLE IF EXISTS node_ca_trust_receipts;
DROP TABLE IF EXISTS node_ca_transition;
DROP TABLE IF EXISTS node_ca_retirement_receipts;
DROP TABLE IF EXISTS node_ca_global_retirement;
DROP TABLE IF EXISTS node_ca_recovered_root;
DROP TABLE IF EXISTS node_ca_operation_requests;

DROP TABLE IF EXISTS recovery_identity_completions;
DROP TABLE IF EXISTS recovery_case_decisions;
DROP TABLE IF EXISTS recovery_case_issues;
DROP TABLE IF EXISTS recovery_cases;

DROP TABLE IF EXISTS owner_expired_certificate_renewals;
DROP TABLE IF EXISTS recovery_owner_credentials;
