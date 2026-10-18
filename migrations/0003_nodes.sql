CREATE TABLE nodes (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','online','degraded','offline','applying','rollback','disabled','error')),
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 hostname TEXT NOT NULL DEFAULT '',
 operating_system TEXT NOT NULL DEFAULT '',
 agent_version TEXT NOT NULL DEFAULT '',
 public_ip TEXT NOT NULL DEFAULT '',
 last_heartbeat TEXT NOT NULL DEFAULT '',
 last_sequence INTEGER NOT NULL DEFAULT 0,
 capabilities_json TEXT NOT NULL DEFAULT '{}',
 heartbeat_json TEXT NOT NULL DEFAULT '{}',
 created_at TEXT NOT NULL
);
CREATE TABLE node_enrollment_tokens (
 token_hash TEXT PRIMARY KEY,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 expires_at TEXT NOT NULL,
 consumed_at TEXT
);
CREATE TABLE node_identities (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 certificate_pem TEXT NOT NULL,
 fingerprint TEXT NOT NULL UNIQUE,
 expires_at TEXT NOT NULL,
 revoked_at TEXT
);
