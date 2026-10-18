CREATE TABLE backup_runs (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('daily','weekly','monthly')),
 period_key TEXT NOT NULL,
 schedule_revision INTEGER NOT NULL CHECK(schedule_revision>0),
 status TEXT NOT NULL CHECK(status IN ('running','success','failed')),
 attempts INTEGER NOT NULL CHECK(attempts>0),
 backup_id TEXT REFERENCES backups(id) ON DELETE SET NULL,
 claim_token TEXT NOT NULL,
 lease_until TEXT NOT NULL,
 last_attempt_at TEXT NOT NULL,
 retry_at TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 UNIQUE(kind,period_key)
);
CREATE INDEX backup_runs_status ON backup_runs(status,last_attempt_at);
