CREATE TABLE recovery_owner_credentials(
 owner_id TEXT NOT NULL REFERENCES admins(id),
 request_key TEXT NOT NULL CHECK(length(request_key)=32),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 original_hash_digest TEXT NOT NULL CHECK(length(original_hash_digest)=64),
 recovered_password_hash TEXT NOT NULL,
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 PRIMARY KEY(owner_id,request_key),
 UNIQUE(owner_id,plan_sha256)
);
