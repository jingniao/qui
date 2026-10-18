CREATE TABLE access_retention_job (
 id INTEGER PRIMARY KEY CHECK(id=1),
 generation INTEGER NOT NULL DEFAULT 0 CHECK(generation>=0),
 phase INTEGER NOT NULL DEFAULT 0 CHECK(phase BETWEEN 0 AND 5),
 status TEXT NOT NULL DEFAULT 'idle' CHECK(status IN ('idle','running','error','success')),
 policy_json TEXT NOT NULL DEFAULT '{}',
 cutoff_at TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT '',
 last_success_at TEXT NOT NULL DEFAULT '',
 deleted_run INTEGER NOT NULL DEFAULT 0 CHECK(deleted_run>=0),
 deleted_total INTEGER NOT NULL DEFAULT 0 CHECK(deleted_total>=0),
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
 error_code TEXT NOT NULL DEFAULT ''
);
INSERT INTO access_retention_job(id) VALUES(1);
