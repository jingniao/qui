-- Access comes only from inbound/import entries. Explicit servers preserve policy scope.
CREATE TABLE node_group_servers (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,node_id)
);
CREATE INDEX node_group_servers_node ON node_group_servers(node_id);
CREATE TABLE node_group_inbounds (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 inbound_id TEXT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,inbound_id)
);
CREATE INDEX node_group_inbounds_inbound ON node_group_inbounds(inbound_id);
CREATE TABLE node_group_imports (
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL REFERENCES subscription_sources(id) ON DELETE CASCADE,
 proxy_key TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(group_id,source_id,proxy_key)
);
INSERT INTO node_group_inbounds
 SELECT m.group_id,i.id FROM node_group_members m JOIN inbounds i ON i.node_id=m.node_id
 JOIN node_groups g ON g.id=m.group_id
 WHERE g.purpose IN ('subscription','mixed') AND i.subscription_visible=1 AND i.internal_relay=0;
INSERT INTO node_group_servers SELECT m.group_id,m.node_id FROM node_group_members m
 WHERE NOT EXISTS(SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id
 WHERE e.group_id=m.group_id AND i.node_id=m.node_id);
DELETE FROM node_group_members;
INSERT INTO node_group_members SELECT group_id,node_id FROM node_group_servers UNION SELECT DISTINCT e.group_id,i.node_id FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id;

CREATE TEMP TABLE k1_import_groups(source_id TEXT,group_id TEXT,name TEXT,created_at TEXT);
INSERT INTO k1_import_groups
 SELECT s.id,lower(hex(randomblob(16))),
 CASE WHEN length(cast('导入 · '||s.name AS BLOB))<=120 THEN '导入 · '||s.name ELSE '导入 · '||substr(s.name,1,27) END,
 s.created_at FROM subscription_sources s
 WHERE EXISTS(SELECT 1 FROM user_source_grants g WHERE g.source_id=s.id);
-- Sources may share names. The generated identity keeps names unique and under
-- the repository's 120-byte limit, including multibyte source names.
UPDATE k1_import_groups AS k SET name=substr(name,1,19)||' · '||group_id
WHERE EXISTS(SELECT 1 FROM node_groups g WHERE g.name=k.name)
OR (SELECT count(*) FROM k1_import_groups c WHERE c.name=k.name)>1;
INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at)
 SELECT group_id,name,'subscription',1,1,created_at FROM k1_import_groups;
INSERT INTO node_group_imports(group_id,source_id) SELECT group_id,source_id FROM k1_import_groups;
INSERT INTO user_node_group_grants SELECT g.user_id,k.group_id FROM user_source_grants g JOIN k1_import_groups k ON k.source_id=g.source_id;
DROP TABLE k1_import_groups;

ALTER TABLE node_meta ADD COLUMN port_range_start INTEGER;
ALTER TABLE node_meta ADD COLUMN port_range_end INTEGER CHECK(
 (port_range_start IS NULL AND port_range_end IS NULL) OR
 (port_range_start IS NOT NULL AND port_range_end IS NOT NULL AND
 typeof(port_range_start)='integer' AND typeof(port_range_end)='integer' AND
 port_range_start BETWEEN 1 AND port_range_end AND port_range_end<=65535));
CREATE TEMP TABLE k1_ports AS
 WITH RECURSIVE positions(node_id,remark,pos) AS (
 SELECT node_id,remark,instr(remark,'[ports:') FROM node_meta
 UNION ALL
 SELECT node_id,remark,pos+6+instr(substr(remark,pos+7),'[ports:') FROM positions
 WHERE pos>0 AND instr(substr(remark,pos+7),'[ports:')>0
 ), suffix AS (
 SELECT node_id,remark,max(pos) pos FROM positions WHERE pos>0 AND substr(remark,-1)=']' GROUP BY node_id
 ), value AS (
 SELECT *,substr(remark,pos+7,length(remark)-pos-7) ports FROM suffix
 ), bounds AS (
 SELECT *,substr(ports,1,instr(ports,'-')-1) a,substr(ports,instr(ports,'-')+1) b FROM value
 ) SELECT node_id,rtrim(substr(remark,1,pos-1)) remark,cast(a AS INTEGER) a,cast(b AS INTEGER) b FROM bounds
 WHERE a<>'' AND b<>'' AND a NOT GLOB '*[^0-9]*' AND b NOT GLOB '*[^0-9]*'
 AND cast(a AS INTEGER) BETWEEN 1 AND cast(b AS INTEGER) AND cast(b AS INTEGER)<=65535;
UPDATE node_meta SET remark=(SELECT p.remark FROM k1_ports p WHERE p.node_id=node_meta.node_id),
 port_range_start=(SELECT p.a FROM k1_ports p WHERE p.node_id=node_meta.node_id),
 port_range_end=(SELECT p.b FROM k1_ports p WHERE p.node_id=node_meta.node_id)
 WHERE node_id IN (SELECT node_id FROM k1_ports);
DROP TABLE k1_ports;

-- These run in repository transactions, including FK cascades and inbound moves.
CREATE TRIGGER node_group_inbounds_insert AFTER INSERT ON node_group_inbounds BEGIN
 INSERT OR IGNORE INTO node_group_members SELECT NEW.group_id,node_id FROM inbounds WHERE id=NEW.inbound_id;
END;
CREATE TRIGGER node_group_inbounds_delete AFTER DELETE ON node_group_inbounds BEGIN
 DELETE FROM node_group_members WHERE group_id=OLD.group_id AND NOT EXISTS(SELECT 1 FROM node_group_servers s WHERE s.group_id=OLD.group_id AND s.node_id=node_group_members.node_id) AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=OLD.group_id AND i.node_id=node_group_members.node_id);
END;
CREATE TRIGGER node_group_inbound_move AFTER UPDATE OF node_id ON inbounds BEGIN
 DELETE FROM node_group_members WHERE NOT EXISTS(SELECT 1 FROM node_group_servers s WHERE s.group_id=node_group_members.group_id AND s.node_id=node_group_members.node_id) AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=node_group_members.group_id AND i.node_id=node_group_members.node_id);
 INSERT OR IGNORE INTO node_group_members SELECT group_id,NEW.node_id FROM node_group_inbounds WHERE inbound_id=NEW.id;
END;

CREATE TRIGGER node_group_servers_insert AFTER INSERT ON node_group_servers BEGIN
 INSERT OR IGNORE INTO node_group_members VALUES(NEW.group_id,NEW.node_id);
END;
CREATE TRIGGER node_group_servers_delete AFTER DELETE ON node_group_servers BEGIN
 DELETE FROM node_group_members WHERE group_id=OLD.group_id AND node_id=OLD.node_id AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=OLD.group_id AND i.node_id=OLD.node_id);
END;
CREATE TRIGGER node_group_servers_update AFTER UPDATE ON node_group_servers BEGIN
 DELETE FROM node_group_members WHERE group_id=OLD.group_id AND node_id=OLD.node_id AND NOT EXISTS(
 SELECT 1 FROM node_group_servers s WHERE s.group_id=OLD.group_id AND s.node_id=OLD.node_id) AND NOT EXISTS(
 SELECT 1 FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=OLD.group_id AND i.node_id=OLD.node_id);
 INSERT OR IGNORE INTO node_group_members VALUES(NEW.group_id,NEW.node_id);
END;
