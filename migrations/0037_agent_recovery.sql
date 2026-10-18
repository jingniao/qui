ALTER TABLE node_update_capabilities ADD COLUMN agent_recovery_supported INTEGER NOT NULL DEFAULT 0 CHECK(agent_recovery_supported IN (0,1));
