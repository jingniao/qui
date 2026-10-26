-- An automatic quota revision may only re-enable an inbound the Owner wants
-- enabled. The renderer disables every inbound that no user credential reaches,
-- so the published state alone cannot tell such an inbound from one the Owner
-- disabled on purpose. This column records the inbounds the Owner had enabled
-- when the revision was created, before that auto-disable; NULL means unknown,
-- which is what revisions created before this column report. Owned and written
-- only by the deployment module.
ALTER TABLE config_revisions ADD COLUMN inbound_enabled_json TEXT;
