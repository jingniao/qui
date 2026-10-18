CREATE TABLE access_space_maintenance (
 id INTEGER PRIMARY KEY CHECK(id=1),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 unconfirmed_attempts INTEGER NOT NULL DEFAULT 0 CHECK(unconfirmed_attempts>=0),
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
 status TEXT NOT NULL DEFAULT 'idle', mode TEXT NOT NULL DEFAULT 'unknown',
 started_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
 last_success_at TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT ''
);
INSERT INTO access_space_maintenance(id) VALUES(1);
