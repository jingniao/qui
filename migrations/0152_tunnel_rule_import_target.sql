-- The migration runner keeps foreign keys enabled inside BEGIN IMMEDIATE.
-- Dropping the old parent cascades into node_group_relays, so preserve those
-- memberships before replacing the table and restore them afterward.
CREATE TEMP TABLE tunnel_rule_import_members AS SELECT group_id,rule_id FROM node_group_relays;

-- Triggers on other tables must not reference a missing tunnel_rules table
-- while SQLite validates the schema for ALTER TABLE RENAME.
DROP TRIGGER tunnel_rule_node_address_changed;
DROP TRIGGER tunnel_rule_inbound_port_changed;

CREATE TABLE tunnel_rules_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 80),
    entry_node_id TEXT NOT NULL REFERENCES nodes(id),
    listen_port INTEGER NOT NULL CHECK(listen_port BETWEEN 1 AND 65535),
    port_mode TEXT NOT NULL CHECK(port_mode IN ('auto','manual')),
    network TEXT NOT NULL CHECK(network IN ('tcp','udp','both')),
    tunnel_id TEXT REFERENCES tunnels(id),
    target_mode TEXT NOT NULL CHECK(target_mode IN ('linked','manual','import')),
    target_inbound_id TEXT REFERENCES inbounds(id),
    target_host TEXT,
    target_port INTEGER CHECK(target_port BETWEEN 1 AND 65535),
    owner_node_id TEXT REFERENCES nodes(id),
    subscription_visible INTEGER NOT NULL CHECK(subscription_visible IN (0,1)),
    enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
    status TEXT NOT NULL CHECK(status IN ('draft','modified','published','needs_publish','pending_delete')),
    draft_version INTEGER NOT NULL DEFAULT 1 CHECK(draft_version>0),
    published_version INTEGER NOT NULL DEFAULT 0 CHECK(published_version>=0),
    published_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_snapshot TEXT,
    target_import_id TEXT REFERENCES imported_proxies(id) ON DELETE SET NULL,
    CHECK((target_mode='linked' AND target_inbound_id IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND target_import_id IS NULL) OR
          (target_mode='manual' AND target_inbound_id IS NULL AND length(target_host)>0 AND target_port IS NOT NULL AND subscription_visible=0 AND target_import_id IS NULL) OR
          (target_mode='import' AND target_inbound_id IS NULL AND target_host IS NULL AND target_port IS NULL))
);
INSERT INTO tunnel_rules_new(id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,owner_node_id,subscription_visible,enabled,status,draft_version,published_version,published_at,created_at,updated_at,published_snapshot)
SELECT id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,owner_node_id,subscription_visible,enabled,status,draft_version,published_version,published_at,created_at,updated_at,published_snapshot FROM tunnel_rules;
DROP TABLE tunnel_rules;
ALTER TABLE tunnel_rules_new RENAME TO tunnel_rules;
CREATE INDEX tunnel_rules_tunnel ON tunnel_rules(tunnel_id);
CREATE INDEX tunnel_rules_target ON tunnel_rules(target_inbound_id);
CREATE INDEX tunnel_rules_import_target ON tunnel_rules(target_import_id);

INSERT INTO node_group_relays(group_id,rule_id) SELECT group_id,rule_id FROM tunnel_rule_import_members;
DROP TABLE tunnel_rule_import_members;

CREATE TRIGGER tunnel_rule_node_address_changed AFTER UPDATE OF public_ip ON nodes
WHEN NEW.public_ip<>OLD.public_ip BEGIN
    UPDATE tunnel_rules SET status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
        draft_version=draft_version+1,
        updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
    WHERE target_inbound_id IN (SELECT id FROM inbounds WHERE node_id=NEW.id) AND status<>'pending_delete';
END;
CREATE TRIGGER tunnel_rule_inbound_port_changed AFTER UPDATE OF port ON inbounds
WHEN NEW.port<>OLD.port BEGIN
    UPDATE tunnel_rules SET status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
        draft_version=draft_version+1,
        updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
    WHERE target_inbound_id=NEW.id AND status<>'pending_delete';
END;

-- A refreshed source may remove an import despite the interactive deletion
-- guard. Keep the rule and its published snapshot, but invalidate its target.
CREATE TRIGGER tunnel_rule_import_target_deleted AFTER UPDATE OF target_import_id ON tunnel_rules
WHEN OLD.target_import_id IS NOT NULL AND NEW.target_import_id IS NULL AND NEW.target_mode='import' BEGIN
    UPDATE tunnel_rules SET status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
        draft_version=draft_version+1,
        updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
    WHERE id=NEW.id AND status<>'pending_delete';
END;
