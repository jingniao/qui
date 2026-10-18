-- W05 step 5: durable Owner operation requests for the fixed offline
-- successor-round CA actions (activate / retire). The panel's own signer and
-- identity directory may only be changed on a stopped panel, so the Owner page
-- records the exact typed request here while the panel runs, a fixed offline
-- CLI command consumes the same request and writes its formal result back to
-- this row, and the page can query the result after restart. This table never
-- executes arbitrary commands, widens signing/candidate/serving permission or
-- replaces the existing readiness/ownership guards; the offline action still
-- re-validates the round, parent proof and current qualifications.
--
-- One request per (plan_digest, operation): a repeat with the same request key
-- reads the retained result, a different key for the same operation is a
-- conflict rather than a second concurrent intent. Appended after 0095; the
-- 0092 recovered-root table and 0049 successor-round table are untouched. The
-- whole-database backup carries this table and the migration is re-applied to
-- the restored candidate, so no separate restore consumer is required.
CREATE TABLE node_ca_operation_requests (
 id INTEGER PRIMARY KEY,
 request_key TEXT NOT NULL UNIQUE CHECK(length(request_key) BETWEEN 16 AND 64),
 plan_digest TEXT NOT NULL CHECK(length(plan_digest)=64),
 operation TEXT NOT NULL CHECK(operation IN ('activate','retire')),
 requested_by TEXT NOT NULL CHECK(length(requested_by)>0),
 requested_by_name TEXT NOT NULL CHECK(length(requested_by_name)>0),
 requested_at TEXT NOT NULL CHECK(length(requested_at)>0),
 status TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed')),
 started_at TEXT NOT NULL DEFAULT '',
 completed_at TEXT NOT NULL DEFAULT '',
 result_json TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL CHECK(length(updated_at)>0),
 UNIQUE(plan_digest, operation)
);
