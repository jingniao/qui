CREATE TABLE node_meta (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 region TEXT NOT NULL DEFAULT '', remark TEXT NOT NULL DEFAULT '',
 renew_date TEXT NOT NULL DEFAULT '', renew_cycle TEXT NOT NULL DEFAULT '',
 renew_price TEXT NOT NULL DEFAULT '', renew_currency TEXT NOT NULL DEFAULT '',
 renew_notice_days INTEGER NOT NULL DEFAULT 7 CHECK(renew_notice_days BETWEEN 0 AND 365),
 last_alert_day TEXT NOT NULL DEFAULT ''
);
