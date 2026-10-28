-- v1.1 end-user accounts (plan sections 4.2 and 6): a proxy user gets a login of
-- their own, separate from the admin accounts, so the portal can show the user
-- their own status, quota and subscription link.
--
-- The password hash uses the same hashing as the admin accounts and is empty
-- until the Owner sets a password; login_enabled stays 0 until then, so an
-- upgrade never opens a login nobody asked for. user_sessions is a separate table
-- from sessions: the two middlewares reject each other's cookies by construction,
-- and a restore does not bring admin sessions back either (internal/panel/auth/
-- recovery.go), so portal sessions end the same way.
--
-- encrypted_token keeps a vault-encrypted copy of the active subscription token
-- next to its hash. The hash stays the lookup key; the encrypted copy is what
-- lets the Owner and the user view the link again (R5, plan C2). Tokens issued
-- before this migration have no copy and report "not recoverable" until reset.

ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN login_enabled INTEGER NOT NULL DEFAULT 0 CHECK(login_enabled IN (0,1));
ALTER TABLE users ADD COLUMN last_login_at TEXT NOT NULL DEFAULT '';

CREATE TABLE user_sessions(
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX user_sessions_expiry ON user_sessions(expires_at);

ALTER TABLE subscription_tokens ADD COLUMN encrypted_token BLOB;
