ALTER TABLE budget_transfer_tasks ADD COLUMN remaining_credit INTEGER NOT NULL DEFAULT 0 CHECK(remaining_credit>=0 AND remaining_credit<=requested_bytes);
UPDATE budget_transfer_tasks SET remaining_credit=CASE WHEN reserved_bytes>0 THEN reserved_bytes ELSE requested_bytes END;
CREATE TABLE budget_transfer_target_returns (
 task_id TEXT NOT NULL REFERENCES budget_transfer_tasks(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL CHECK(generation>=1),
 return_task_id TEXT NOT NULL UNIQUE REFERENCES budget_return_tasks(id),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 budget_sequence INTEGER NOT NULL CHECK(budget_sequence>=1),
 budget_ceiling INTEGER NOT NULL CHECK(budget_ceiling>=0),
 transfer_bytes INTEGER NOT NULL CHECK(transfer_bytes>0 AND transfer_bytes<=budget_ceiling),
 credit_before INTEGER NOT NULL CHECK(credit_before>=0 AND credit_before<=transfer_bytes),
 credit_after INTEGER CHECK(credit_after>=0 AND credit_after<=credit_before),
 status TEXT NOT NULL CHECK(status IN ('closing','closed')),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK((status='closing' AND credit_after IS NULL) OR (status='closed' AND credit_after IS NOT NULL)),
 PRIMARY KEY(task_id,generation),
 FOREIGN KEY(budget_id,budget_sequence) REFERENCES user_node_budget_versions(budget_id,sequence)
);
