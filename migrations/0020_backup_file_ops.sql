CREATE TABLE backup_file_ops (
 backup_id TEXT PRIMARY KEY,
 operation TEXT NOT NULL CHECK(operation IN ('create','delete')),
 phase TEXT NOT NULL CHECK(phase IN ('prepared','committed')),
 metadata_json TEXT NOT NULL,
 created_at TEXT NOT NULL
);
