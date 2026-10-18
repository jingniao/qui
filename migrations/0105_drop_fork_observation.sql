-- Phase 7B: the fork-only connection-observation and egress-association tables.
--
-- The patched cores that emitted PROXYCONTROL-OBS frames are gone (plan D1), and
-- Phase 7A takes access records from the upstream sources instead. Nothing
-- reads or writes these tables any more: the panel's observation read side, the
-- Agent's observation queue and the restore preservation of these tables were
-- removed in the same commit. DROP is safe for an existing database, and a
-- pre-refactor backup still restores because migrations run forward on the
-- staged copy.
DROP TABLE IF EXISTS egress_replay_guards;
DROP TABLE IF EXISTS egress_replay_sources;
DROP TABLE IF EXISTS egress_associations;
DROP TABLE IF EXISTS connection_observation_gaps;
DROP TABLE IF EXISTS connection_observation_cursors;
DROP TABLE IF EXISTS connection_observations;