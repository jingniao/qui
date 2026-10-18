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
 status TEXT NOT NULL CHECK(status IN ('returning','publishing','complete','blocked','cancelled','reclaimed')),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 generation INTEGER NOT NULL DEFAULT 1 CHECK(generation>=1),
 initial_target_node_id TEXT NOT NULL REFERENCES nodes(id),
 CHECK(source_node_id<>target_node_id),
 CHECK((reserved_bytes=0 AND target_budget_id IS NULL AND target_sequence IS NULL AND target_ceiling_bytes IS NULL AND target_revision IS NULL AND status IN ('returning','cancelled')) OR
       (reserved_bytes>0 AND target_budget_id IS NOT NULL AND target_sequence IS NOT NULL AND target_sequence>=1 AND target_ceiling_bytes IS NOT NULL AND target_ceiling_bytes>=reserved_bytes AND status IN ('publishing','complete','blocked','reclaimed'))),
 CHECK(status<>'complete' OR target_revision IS NOT NULL),
 UNIQUE(user_id,source_node_id,source_sequence),
 FOREIGN KEY(target_budget_id,target_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
INSERT INTO budget_transfer_tasks_next SELECT t.*,1,t.target_node_id FROM budget_transfer_tasks t;
DROP TABLE budget_transfer_tasks;
ALTER TABLE budget_transfer_tasks_next RENAME TO budget_transfer_tasks;
CREATE UNIQUE INDEX active_budget_transfer_user ON budget_transfer_tasks(user_id) WHERE status NOT IN ('complete','cancelled','reclaimed');
CREATE UNIQUE INDEX active_budget_transfer_target ON budget_transfer_tasks(target_node_id) WHERE status NOT IN ('complete','cancelled','reclaimed');

CREATE TABLE budget_transfer_reclaims (
 task_id TEXT NOT NULL REFERENCES budget_transfer_tasks(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL CHECK(generation>=1),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 withdrawn_sequence INTEGER NOT NULL CHECK(withdrawn_sequence>=1),
 replacement_sequence INTEGER NOT NULL CHECK(replacement_sequence=withdrawn_sequence+1),
 withdrawn_ceiling INTEGER NOT NULL CHECK(withdrawn_ceiling>=0),
 replacement_ceiling INTEGER NOT NULL CHECK(replacement_ceiling>=0 AND replacement_ceiling<withdrawn_ceiling),
 returned_bytes INTEGER NOT NULL CHECK(returned_bytes=withdrawn_ceiling-replacement_ceiling),
 reported_spent INTEGER NOT NULL CHECK(reported_spent>=0 AND reported_spent<=replacement_ceiling),
 previous_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 target_revision INTEGER REFERENCES config_revisions(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(task_id,generation),
 UNIQUE(budget_id,withdrawn_sequence),
 FOREIGN KEY(budget_id,withdrawn_sequence) REFERENCES user_node_budget_versions(budget_id,sequence),
 FOREIGN KEY(budget_id,replacement_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
CREATE TABLE budget_transfer_redirects (
 task_id TEXT NOT NULL REFERENCES budget_transfer_tasks(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL CHECK(generation>=1),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 previous_revision INTEGER NOT NULL REFERENCES config_revisions(id),
 reserved_bytes INTEGER NOT NULL CHECK(reserved_bytes>=0),
 budget_id TEXT REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 budget_sequence INTEGER,
 budget_ceiling INTEGER,
 target_revision INTEGER REFERENCES config_revisions(id),
 created_at TEXT NOT NULL,
 CHECK((reserved_bytes=0 AND budget_id IS NULL AND budget_sequence IS NULL AND budget_ceiling IS NULL AND target_revision IS NULL) OR
       (reserved_bytes>0 AND budget_id IS NOT NULL AND budget_sequence IS NOT NULL AND budget_sequence>=1 AND budget_ceiling IS NOT NULL AND budget_ceiling>=reserved_bytes)),
 PRIMARY KEY(task_id,generation),
 FOREIGN KEY(budget_id,budget_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
