-- Phase 5B: live connection counts are observed in panel memory.
-- The per-node live connection tables are dead; drop them.
DROP TABLE IF EXISTS node_live_connection_accounts;
DROP TABLE IF EXISTS node_live_connections;
