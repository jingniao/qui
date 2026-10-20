-- Node deletion is logical. deleted_at keeps the row (and every history row that
-- references it) while the node leaves lists, node groups and subscriptions, and
-- its certificate stays revoked. NULL means "not deleted".
ALTER TABLE nodes ADD COLUMN deleted_at TEXT;
