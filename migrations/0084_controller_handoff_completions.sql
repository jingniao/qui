CREATE TABLE controller_handoff_completions(
 approval_sha256 TEXT PRIMARY KEY CHECK(length(approval_sha256)=64),
 owner TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64),
 result_json TEXT NOT NULL CHECK(length(CAST(result_json AS BLOB))<=8192),
 UNIQUE(owner,request_key)
);
