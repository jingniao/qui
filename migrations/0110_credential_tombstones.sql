-- A deleted user or device leaves its credentials behind in the revisions the
-- nodes still run, so traffic counters for those credentials keep arriving after
-- the rows are gone. These tombstones keep the identity of every removed
-- credential, which lets the user module accept such a counter for accounting
-- (advance its cursor, charge the rows that still exist) instead of rejecting a
-- batch the node can never repair. Owned and written only by the user module.
CREATE TABLE credential_tombstones (
  credential_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  deleted_at TEXT NOT NULL
);
CREATE INDEX credential_tombstones_identity ON credential_tombstones(user_id,device_id);
