CREATE TABLE traffic_receipt_archives (
 node_id TEXT NOT NULL REFERENCES nodes(id),
 first_id TEXT NOT NULL,
 last_id TEXT NOT NULL,
 records INTEGER NOT NULL CHECK(records BETWEEN 1 AND 128),
 digest TEXT NOT NULL,
 payload BLOB NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(node_id,first_id),
 CHECK(first_id<=last_id)
);
CREATE INDEX traffic_receipt_archive_end ON traffic_receipt_archives(node_id,last_id);
