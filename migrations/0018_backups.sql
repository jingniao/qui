CREATE TABLE backups (
 id TEXT PRIMARY KEY,
 created_at TEXT NOT NULL,
 created_by TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('manual','daily','weekly','monthly')),
 format_version INTEGER NOT NULL CHECK(format_version=1),
 file_bytes INTEGER NOT NULL CHECK(file_bytes>0),
 sha256 TEXT NOT NULL CHECK(length(sha256)=64),
 verified_at TEXT NOT NULL
);
CREATE INDEX backups_created_at ON backups(created_at);
