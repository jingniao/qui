CREATE TABLE tunnels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE CHECK(length(trim(name)) BETWEEN 1 AND 80),
    transport TEXT NOT NULL CHECK(transport='tls'),
    status TEXT NOT NULL CHECK(status IN ('draft','modified','published','needs_publish','pending_delete')),
    draft_version INTEGER NOT NULL DEFAULT 1 CHECK(draft_version>0),
    published_version INTEGER NOT NULL DEFAULT 0 CHECK(published_version>=0),
    published_at TEXT,
    gost_material BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE tunnel_hops (
    tunnel_id TEXT NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK(role IN ('entry','mid','exit')),
    position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 2),
    node_id TEXT NOT NULL REFERENCES nodes(id),
    listen_port INTEGER CHECK(listen_port BETWEEN 1 AND 65535),
    port_mode TEXT NOT NULL CHECK(port_mode IN ('auto','manual')),
    connect_host TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(tunnel_id,role),
    UNIQUE(tunnel_id,position),
    UNIQUE(tunnel_id,node_id),
    CHECK((role='entry' AND position=0 AND listen_port IS NULL) OR (role<>'entry' AND position>0 AND listen_port IS NOT NULL))
);
CREATE TABLE tunnel_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 80),
    entry_node_id TEXT NOT NULL REFERENCES nodes(id),
    listen_port INTEGER NOT NULL CHECK(listen_port BETWEEN 1 AND 65535),
    port_mode TEXT NOT NULL CHECK(port_mode IN ('auto','manual')),
    network TEXT NOT NULL CHECK(network IN ('tcp','udp','both')),
    tunnel_id TEXT REFERENCES tunnels(id),
    target_mode TEXT NOT NULL CHECK(target_mode IN ('linked','manual')),
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
    CHECK((target_mode='linked' AND target_inbound_id IS NOT NULL AND target_host IS NULL AND target_port IS NULL) OR
          (target_mode='manual' AND target_inbound_id IS NULL AND length(target_host)>0 AND target_port IS NOT NULL AND subscription_visible=0))
);
CREATE INDEX tunnel_rules_tunnel ON tunnel_rules(tunnel_id);
CREATE INDEX tunnel_rules_target ON tunnel_rules(target_inbound_id);
CREATE TABLE port_claims (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
    owner_kind TEXT NOT NULL CHECK(owner_kind IN ('tunnel_hop','rule','canvas')),
    owner_id TEXT NOT NULL,
    PRIMARY KEY(node_id,port)
);
CREATE INDEX port_claims_owner ON port_claims(owner_kind,owner_id);

CREATE TABLE tunnel_objects (
    object_kind TEXT NOT NULL CHECK(object_kind='inbound'),
    object_id TEXT NOT NULL UNIQUE REFERENCES inbounds(id) ON DELETE CASCADE,
    owner_kind TEXT NOT NULL CHECK(owner_kind IN ('tunnel','rule')),
    owner_id TEXT NOT NULL,
    PRIMARY KEY(object_kind,object_id)
);
CREATE INDEX tunnel_objects_owner ON tunnel_objects(owner_kind,owner_id);

-- Targets are derived on read. Invalidation covers settings, heartbeat updates,
-- and repository writes in the same transaction as the destination change.
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
