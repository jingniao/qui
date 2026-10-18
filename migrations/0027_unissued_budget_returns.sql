ALTER TABLE quota_conversion_targets ADD COLUMN returned_budget_sequence INTEGER NOT NULL DEFAULT 0 CHECK(returned_budget_sequence>=0);
CREATE TABLE unissued_budget_returns (
 conversion_id TEXT NOT NULL REFERENCES quota_conversions(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 returned_sequence INTEGER NOT NULL CHECK(returned_sequence>=1),
 replacement_sequence INTEGER NOT NULL CHECK(replacement_sequence=returned_sequence+1),
 returned_bytes INTEGER NOT NULL CHECK(returned_bytes>=0),
 pause_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(conversion_id,node_id),
 UNIQUE(budget_id,returned_sequence),
 FOREIGN KEY(budget_id,returned_sequence) REFERENCES user_node_budget_versions(budget_id,sequence),
 FOREIGN KEY(budget_id,replacement_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
