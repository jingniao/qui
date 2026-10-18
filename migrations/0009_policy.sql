CREATE TABLE policies (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, priority INTEGER NOT NULL CHECK(priority BETWEEN 0 AND 1000000),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), scope_json TEXT NOT NULL, match_json TEXT NOT NULL,
 action_json TEXT NOT NULL, logging_json TEXT NOT NULL, timezone TEXT NOT NULL DEFAULT 'UTC',
 remark TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX policy_priority ON policies(priority,id);
CREATE TABLE service_sets (
 id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, builtin INTEGER NOT NULL CHECK(builtin IN (0,1)),
 version INTEGER NOT NULL DEFAULT 1, enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE service_set_entries (
 id INTEGER PRIMARY KEY, service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('domain','suffix','cidr')), value TEXT NOT NULL,
 UNIQUE(service_id,kind,value)
);
INSERT INTO service_sets(id,name,builtin,enabled,description,created_at,updated_at) VALUES
 ('service-netflix','Netflix',1,1,'内置基础域名集合，变更后需配置发布',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('service-disney','Disney+',1,1,'内置基础域名集合',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('service-youtube','YouTube',1,1,'内置基础域名集合',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('service-openai','OpenAI',1,1,'内置基础域名集合',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('service-google','Google',1,1,'内置基础域名集合',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('service-github','GitHub',1,1,'内置基础域名集合',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')),
 ('service-telegram','Telegram',1,1,'内置基础域名集合',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now'));
INSERT INTO service_set_entries(service_id,kind,value) VALUES
 ('service-netflix','suffix','netflix.com'),('service-netflix','suffix','nflxvideo.net'),('service-netflix','suffix','nflximg.net'),('service-netflix','suffix','nflxext.com'),
 ('service-disney','suffix','disneyplus.com'),('service-disney','suffix','dssott.com'),('service-disney','suffix','disney-plus.net'),
 ('service-youtube','suffix','youtube.com'),('service-youtube','suffix','googlevideo.com'),('service-youtube','suffix','ytimg.com'),('service-youtube','suffix','youtu.be'),
 ('service-openai','suffix','openai.com'),('service-openai','suffix','chatgpt.com'),('service-openai','suffix','oaistatic.com'),('service-openai','suffix','oaiusercontent.com'),
 ('service-google','suffix','google.com'),('service-google','suffix','gstatic.com'),('service-google','suffix','googleapis.com'),
 ('service-github','suffix','github.com'),('service-github','suffix','githubusercontent.com'),('service-github','suffix','githubassets.com'),
 ('service-telegram','suffix','telegram.org'),('service-telegram','suffix','t.me'),('service-telegram','suffix','tdesktop.com');
