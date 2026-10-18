-- Owned by the user module. Issued ceilings remain reserved until an explicit
-- node-confirmed release; an offline node or failed deployment is not a release.
CREATE TABLE user_node_budgets (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id),
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 ceiling_bytes INTEGER NOT NULL CHECK(ceiling_bytes>=0),
 reported_spent INTEGER NOT NULL DEFAULT 0 CHECK(reported_spent>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(user_id,node_id)
);
CREATE TABLE user_node_budget_versions (
 budget_id TEXT NOT NULL REFERENCES user_node_budgets(id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL CHECK(sequence>=1),
 ceiling_bytes INTEGER NOT NULL CHECK(ceiling_bytes>=0),
 PRIMARY KEY(budget_id,sequence)
);
