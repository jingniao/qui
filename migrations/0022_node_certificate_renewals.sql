CREATE TABLE node_certificate_renewals (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 previous_fingerprint TEXT NOT NULL,
 certificate_pem TEXT NOT NULL,
 fingerprint TEXT NOT NULL UNIQUE,
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
);
