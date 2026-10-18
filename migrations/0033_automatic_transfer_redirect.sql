ALTER TABLE budget_transfer_tasks ADD COLUMN auto_redirect INTEGER NOT NULL DEFAULT 0 CHECK(auto_redirect IN (0,1));
ALTER TABLE budget_transfer_tasks ADD COLUMN initial_auto_redirect INTEGER NOT NULL DEFAULT 0 CHECK(initial_auto_redirect IN (0,1));
ALTER TABLE budget_transfer_tasks ADD COLUMN auto_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE budget_transfer_redirects ADD COLUMN automatic INTEGER NOT NULL DEFAULT 0 CHECK(automatic IN (0,1));
CREATE TABLE budget_transfer_auto_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 task_id TEXT NOT NULL REFERENCES budget_transfer_tasks(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL CHECK(generation>=1),
 node_id TEXT NOT NULL REFERENCES nodes(id),
 code TEXT NOT NULL CHECK(code IN ('enabled','disabled','selected','no_candidate','waiting_return','waiting_application','allowance_changed','no_credit','eligible')),
 created_at TEXT NOT NULL
);
CREATE INDEX transfer_auto_events_task ON budget_transfer_auto_events(task_id,id);
