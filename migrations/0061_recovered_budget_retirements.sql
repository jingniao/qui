CREATE TABLE recovered_budget_retirements (
 request_id TEXT PRIMARY KEY,
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE RESTRICT,
 user_id TEXT NOT NULL,
 node_id TEXT NOT NULL,
 returned_sequence INTEGER NOT NULL,
 replacement_sequence INTEGER NOT NULL CHECK(replacement_sequence=returned_sequence+1),
 spent_bytes INTEGER NOT NULL CHECK(spent_bytes>=0),
 returned_bytes INTEGER NOT NULL CHECK(returned_bytes>=0),
 case_id TEXT NOT NULL,
 decision_id TEXT NOT NULL,
 evidence_digest TEXT NOT NULL CHECK(length(evidence_digest)=64),
 receipt_digest TEXT NOT NULL CHECK(length(receipt_digest)=64),
 encrypted_receipt BLOB NOT NULL,
 owner TEXT NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(budget_id,returned_sequence)
);
