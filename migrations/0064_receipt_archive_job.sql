CREATE TABLE receipt_archive_job (
 id INTEGER PRIMARY KEY CHECK(id=1),
 version INTEGER NOT NULL DEFAULT 0 CHECK(version>=0),
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 state_json TEXT NOT NULL DEFAULT '{"status":"idle","kind":"access"}'
);
INSERT INTO receipt_archive_job(id) VALUES(1);
