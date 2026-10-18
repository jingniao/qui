CREATE TABLE app_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    created_at TEXT NOT NULL
);
INSERT INTO app_metadata (id, created_at)
VALUES (1, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
