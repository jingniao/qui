-- Rebuild transactionally to extend CHECK constraints while preserving every
-- existing column, task and grant reference. There are no incoming task FKs.
CREATE TABLE budget_transfer_tasks_next (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 source_node_id TEXT NOT NULL REFERENCES nodes(id),
 source_sequence INTEGER NOT NULL CHECK(source_sequence>=1),
 return_task_id TEXT NOT NULL UNIQUE REFERENCES budget_return_tasks(id),
 target_node_id TEXT NOT NULL REFERENCES nodes(id),
 target_previous_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 requested_bytes INTEGER NOT NULL CHECK(requested_bytes>0),
 reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK(reserved_bytes>=0 AND reserved_bytes<=requested_bytes),
 target_budget_id TEXT REFERENCES user_node_budgets(id),
 target_sequence INTEGER,
 target_ceiling_bytes INTEGER,
 target_revision INTEGER REFERENCES config_revisions(id),
 status TEXT NOT NULL CHECK(status IN ('returning','publishing','complete','blocked','cancelled')),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(source_node_id<>target_node_id),
 CHECK((reserved_bytes=0 AND target_budget_id IS NULL AND target_sequence IS NULL AND target_ceiling_bytes IS NULL AND target_revision IS NULL AND status IN ('returning','cancelled')) OR
       (reserved_bytes>0 AND target_budget_id IS NOT NULL AND target_sequence IS NOT NULL AND target_sequence>=1 AND target_ceiling_bytes IS NOT NULL AND target_ceiling_bytes>=reserved_bytes AND status IN ('publishing','complete','blocked'))),
 CHECK(status<>'complete' OR target_revision IS NOT NULL),
 UNIQUE(user_id,source_node_id,source_sequence),
 FOREIGN KEY(target_budget_id,target_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
INSERT INTO budget_transfer_tasks_next SELECT * FROM budget_transfer_tasks;
DROP TABLE budget_transfer_tasks;
ALTER TABLE budget_transfer_tasks_next RENAME TO budget_transfer_tasks;
CREATE UNIQUE INDEX active_budget_transfer_user ON budget_transfer_tasks(user_id) WHERE status NOT IN ('complete','cancelled');
CREATE UNIQUE INDEX active_budget_transfer_target ON budget_transfer_tasks(target_node_id) WHERE status NOT IN ('complete','cancelled');
