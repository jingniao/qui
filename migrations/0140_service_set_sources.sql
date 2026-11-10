CREATE TABLE service_set_entries_new (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('domain','suffix','cidr','keyword')),
    value TEXT NOT NULL,
    UNIQUE(service_id,kind,value)
);
INSERT INTO service_set_entries_new(id,service_id,kind,value) SELECT id,service_id,kind,value FROM service_set_entries;
DROP TABLE service_set_entries;
ALTER TABLE service_set_entries_new RENAME TO service_set_entries;

CREATE TABLE service_set_sources (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    format TEXT NOT NULL CHECK(format IN ('yaml','text','mrs')),
    behavior TEXT NOT NULL CHECK(behavior IN ('classical','domain','ipcidr')),
    interval_seconds INTEGER NOT NULL DEFAULT 86400 CHECK(interval_seconds BETWEEN 3600 AND 604800),
    position INTEGER NOT NULL,
    last_status TEXT NOT NULL DEFAULT 'pending' CHECK(last_status IN ('pending','ok','failed')),
    last_error_code TEXT NOT NULL DEFAULT '',
    last_fetch_at TEXT,
    last_success_at TEXT,
    entry_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    encrypted_cache BLOB,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK(format <> 'mrs' OR behavior IN ('domain','ipcidr'))
);
CREATE INDEX service_set_sources_service ON service_set_sources(service_id, position);
