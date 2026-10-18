CREATE TABLE admins (
 id TEXT PRIMARY KEY,
 username TEXT NOT NULL UNIQUE,
 display_name TEXT NOT NULL DEFAULT '',
 role TEXT NOT NULL CHECK (role IN ('Owner', 'Viewer')),
 status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
 password_hash TEXT NOT NULL,
 created_at TEXT NOT NULL,
 last_login_at TEXT NOT NULL DEFAULT '',
 login_ip TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX one_owner ON admins(role) WHERE role = 'Owner';
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 admin_id TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE login_attempts (
 id INTEGER PRIMARY KEY,
 ip TEXT NOT NULL,
 attempted_at TEXT NOT NULL
);
CREATE INDEX login_attempts_window ON login_attempts(ip, attempted_at);
CREATE TABLE admin_operations (
 id INTEGER PRIMARY KEY,
 timestamp TEXT NOT NULL,
 operator TEXT NOT NULL,
 action TEXT NOT NULL,
 target TEXT NOT NULL,
 result TEXT NOT NULL CHECK (result IN ('成功','失败')),
 ip TEXT NOT NULL
);
CREATE INDEX admin_operations_time ON admin_operations(timestamp);
CREATE TABLE system_settings (
 key TEXT PRIMARY KEY,
 value_json TEXT NOT NULL
);
