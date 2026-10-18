-- Phase 4B: retire per-node budgets and the connection-admission ledger.
--
-- Phase 4A/4B removed budget grants, connection admission and final-usage
-- settlement from the panel and the agent, then dropped their business data
-- from the API surface. The tables those subsystems created are no longer
-- written or read by any retained code, so drop them here. Every table below
-- was created by migrations 0017 and 0023-0034, 0038-0039 and 0061.
--
-- Dropped here:
--   user_node_budgets, user_node_budget_versions   -> per-node budget ledger
--   quota_conversions, quota_conversion_targets    -> quota conversion ledger
--   unissued_budget_returns                        -> unissued return ledger
--   budget_return_tasks, budget_return_commands,
--   issued_budget_returns                          -> budget return ledger
--   budget_transfer_tasks, budget_transfer_reclaims,
--   budget_transfer_redirects,
--   budget_transfer_target_returns,
--     budget_transfer_auto_events                  -> budget transfer ledger
--   recovered_budget_retirements                   -> recovered budget returns
--   deployment_final_usage, runtime_final_usage    -> final-usage settlement
--   runtime_sources                                -> runtime source accounting
--   node_pause_tasks, node_pause_attempts          -> budget-driven node pauses
--   user_connection_limits, user_node_connections,
--   user_node_connection_retirements               -> connection-admission ledger
--
-- Column additions that several of these migrations made to still-live tables
-- (for example the budget/connection columns added to nodes or users) are kept;
-- the migration recreates no columns and only removes whole tables.
--
-- Kept because retained code still reads them:
--   node_live_connections, node_live_connection_accounts
--     -> dashboard/repository.go (current-connection measurement) and
--        backup/restore prepare clearing. Their writer moved out in phase 4B.2a
--        but the read paths remain, so they are out of scope for this drop.
--
-- Children are dropped before their parents so the foreign keys declared with
-- ON DELETE CASCADE / ON DELETE RESTRICT never fire during removal.

DROP TABLE IF EXISTS budget_transfer_auto_events;
DROP TABLE IF EXISTS budget_transfer_target_returns;
DROP TABLE IF EXISTS budget_transfer_redirects;
DROP TABLE IF EXISTS budget_transfer_reclaims;
DROP TABLE IF EXISTS budget_transfer_tasks;
DROP TABLE IF EXISTS issued_budget_returns;
DROP TABLE IF EXISTS budget_return_commands;
DROP TABLE IF EXISTS budget_return_tasks;
DROP TABLE IF EXISTS unissued_budget_returns;
DROP TABLE IF EXISTS recovered_budget_retirements;
DROP TABLE IF EXISTS quota_conversion_targets;
DROP TABLE IF EXISTS quota_conversions;
DROP TABLE IF EXISTS user_node_budget_versions;
DROP TABLE IF EXISTS user_node_budgets;

DROP TABLE IF EXISTS deployment_final_usage;
DROP TABLE IF EXISTS runtime_final_usage;
DROP TABLE IF EXISTS runtime_sources;

DROP TABLE IF EXISTS node_pause_attempts;
DROP TABLE IF EXISTS node_pause_tasks;

DROP TABLE IF EXISTS user_node_connection_retirements;
DROP TABLE IF EXISTS user_node_connections;
DROP TABLE IF EXISTS user_connection_limits;
