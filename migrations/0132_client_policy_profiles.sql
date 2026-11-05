CREATE TABLE client_policy_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    template_json TEXT NOT NULL,
    ui_metadata_json TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    import_key TEXT UNIQUE,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by TEXT NOT NULL
);
