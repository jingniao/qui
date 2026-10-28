CREATE TABLE subscription_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url_encrypted BLOB NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 60 AND 2592000),
    last_fetch_at TEXT NOT NULL DEFAULT '',
    last_status TEXT NOT NULL DEFAULT 'pending' CHECK(last_status IN ('pending','ok','failed')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE imported_proxies (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES subscription_sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL,
    payload_encrypted BLOB NOT NULL,
    fetched_at TEXT NOT NULL
);
CREATE TABLE user_source_grants (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES subscription_sources(id) ON DELETE CASCADE,
    PRIMARY KEY(user_id, source_id)
);
ALTER TABLE canvas_schemes ADD COLUMN client_published_layout TEXT NOT NULL DEFAULT '';
