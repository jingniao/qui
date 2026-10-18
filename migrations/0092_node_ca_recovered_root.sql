-- W05 first current-valid recovered root publication.
--
-- A single durable row binds the brand-new self-signed recovered root candidate
-- to the original expired root material and to the original Owner approval. It
-- is append-only relative to the existing highest migration 0091 and does not
-- alter node_ca_transition, node_ca_global_retirement or any successor round
-- table. The row records the staged phases approved -> installing -> active so an
-- interrupted offline install resumes with the same exact candidate instead of
-- replacing trust, and the original ca.crt/ca.key history is preserved by the
-- fixed command outside this table.
CREATE TABLE node_ca_recovered_root (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 original_fingerprint TEXT NOT NULL CHECK(length(original_fingerprint)=64),
 candidate_fingerprint TEXT NOT NULL CHECK(length(candidate_fingerprint)=64),
 candidate_key_digest TEXT NOT NULL CHECK(length(candidate_key_digest)=64),
 owner_id TEXT NOT NULL CHECK(length(owner_id)>0),
 owner_name TEXT NOT NULL CHECK(length(owner_name)>0),
 request_key TEXT NOT NULL CHECK(length(request_key) BETWEEN 16 AND 64),
 stage_directory TEXT NOT NULL CHECK(length(stage_directory)>0),
 database_path TEXT NOT NULL CHECK(length(database_path)>0),
 identity_path TEXT NOT NULL CHECK(length(identity_path)>0),
 status TEXT NOT NULL CHECK (status IN ('approved','installing','active')),
 approved_at TEXT NOT NULL CHECK(length(approved_at)>0),
 installed_at TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL CHECK(length(created_at)>0)
);
