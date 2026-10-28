ALTER TABLE users ADD COLUMN traffic_reset_cycle TEXT NOT NULL DEFAULT 'none' CHECK(traffic_reset_cycle IN ('none','monthly','30days'));
ALTER TABLE users ADD COLUMN traffic_reset_anchor TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN traffic_reset_at TEXT NOT NULL DEFAULT '';

-- Only a leading, closed config prefix is metadata. Last recognized cycle wins,
-- matching the former UI parser. Unknown keys (including up/down) are removed.
WITH RECURSIVE parts(id,rest,part,n) AS (
 SELECT id,substr(remark,9,instr(remark,']')-9)||';','',0 FROM users
 WHERE substr(remark,1,8)='[config:' AND instr(remark,']')>9
 UNION ALL
 SELECT id,substr(rest,instr(rest,';')+1),substr(rest,1,instr(rest,';')-1),n+1
 FROM parts WHERE rest<>''
)
UPDATE users SET traffic_reset_cycle=coalesce((
 SELECT substr(part,7) FROM parts WHERE parts.id=users.id
 AND part IN ('cycle=monthly','cycle=30days') ORDER BY n DESC LIMIT 1),'none'),
 traffic_reset_anchor=created_at,
 remark=ltrim(substr(remark,instr(remark,']')+1),char(9)||char(10)||char(11)||char(12)||char(13)||' ')
WHERE substr(remark,1,8)='[config:' AND instr(remark,']')>9;
