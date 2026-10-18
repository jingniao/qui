ALTER TABLE nodes ADD COLUMN budget_retirement_supported INTEGER NOT NULL DEFAULT 0 CHECK(budget_retirement_supported IN (0,1));
CREATE TABLE budget_return_tasks (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 ceiling_bytes INTEGER NOT NULL CHECK(ceiling_bytes>=0),
 previous_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 paused_revision INTEGER REFERENCES config_revisions(id),
 command_id TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('pausing','settling','closing','confirmed','complete','blocked')),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(budget_id,sequence)
);
CREATE UNIQUE INDEX active_budget_return_node ON budget_return_tasks(node_id) WHERE status<>'complete';
CREATE TABLE budget_return_commands (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES budget_return_tasks(id) ON DELETE CASCADE,
 encrypted_request BLOB NOT NULL,
 encrypted_receipt BLOB,
 status TEXT NOT NULL CHECK(status IN ('pending','success','failed')),
 code TEXT NOT NULL DEFAULT ''
);
CREATE TABLE issued_budget_returns (
 task_id TEXT PRIMARY KEY REFERENCES budget_return_tasks(id) ON DELETE CASCADE,
 command_id TEXT NOT NULL UNIQUE REFERENCES budget_return_commands(id) ON DELETE CASCADE,
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 returned_sequence INTEGER NOT NULL CHECK(returned_sequence>=1),
 replacement_sequence INTEGER NOT NULL CHECK(replacement_sequence=returned_sequence+1),
 spent_bytes INTEGER NOT NULL CHECK(spent_bytes>=0),
 returned_bytes INTEGER NOT NULL CHECK(returned_bytes>=0),
 created_at TEXT NOT NULL,
 UNIQUE(budget_id,returned_sequence),
 FOREIGN KEY(budget_id,returned_sequence) REFERENCES user_node_budget_versions(budget_id,sequence),
 FOREIGN KEY(budget_id,replacement_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
