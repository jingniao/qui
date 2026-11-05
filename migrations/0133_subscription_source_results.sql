-- S1: keep the last refresh result and the last successful refresh per source, so
-- the Owner can tell a failed attempt from a successful one and bounded per-entry
-- diagnostics survive a page reload. Both defaults are the empty string, so a
-- source written before this migration, and an archive that predates it, reads as
-- "no recorded result" instead of an invented cause.
ALTER TABLE subscription_sources ADD COLUMN latest_result_json TEXT NOT NULL DEFAULT '';
ALTER TABLE subscription_sources ADD COLUMN last_success_at TEXT NOT NULL DEFAULT '';
