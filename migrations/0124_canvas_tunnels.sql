-- Server connections become independent resources. Go performs the one-time
-- exact-name, unambiguous legacy adoption before the first canvas mutation.
CREATE TABLE canvas_tunnels (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 connection_json TEXT NOT NULL CHECK(json_valid(connection_json)),
 source_ref TEXT NOT NULL,
 target_ref TEXT NOT NULL,
 inline_scheme_id TEXT NOT NULL DEFAULT '',
 object_prefix TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 published_version TEXT NOT NULL DEFAULT '',
 allocated_port INTEGER CHECK(allocated_port IS NULL OR allocated_port BETWEEN 1 AND 65535)
);
CREATE TABLE canvas_tunnel_objects (
 tunnel_id TEXT NOT NULL REFERENCES canvas_tunnels(id) ON DELETE RESTRICT,
 object_kind TEXT NOT NULL CHECK(object_kind IN ('inbound','outbound','chain')),
 object_id TEXT NOT NULL,
 PRIMARY KEY(object_kind,object_id)
);
CREATE INDEX canvas_tunnel_objects_owner ON canvas_tunnel_objects(tunnel_id);
CREATE TABLE canvas_tunnel_publications (
 scheme_id TEXT NOT NULL REFERENCES canvas_schemes(id) ON DELETE CASCADE,
 tunnel_id TEXT NOT NULL REFERENCES canvas_tunnels(id) ON DELETE CASCADE,
 PRIMARY KEY(scheme_id,tunnel_id)
);
CREATE TABLE canvas_tunnel_reconcile (id INTEGER PRIMARY KEY CHECK(id=1), done INTEGER NOT NULL CHECK(done IN(0,1)));
INSERT INTO canvas_tunnel_reconcile VALUES(1,0);
INSERT INTO canvas_tunnels(id,name,connection_json,source_ref,target_ref,inline_scheme_id,object_prefix,created_at,updated_at)
SELECT s.id || '.' || c.key, s.id || '.' || c.key,
 CASE WHEN json_extract(c.value,'$.type')='port_forward' THEN json_set(c.value,
 '$.listenPort',COALESCE(json_extract(c.value,'$.listenPort'),(SELECT json_extract(i.value,'$.listenPort') FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.instanceId')=json_extract(c.value,'$.source'))),
 '$.targetPort',COALESCE(json_extract(c.value,'$.targetPort'),(SELECT json_extract(i.value,'$.targetPort') FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.instanceId')=json_extract(c.value,'$.target')))) ELSE c.value END,
 COALESCE((SELECT json_extract(i.value,'$.ref') FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.instanceId')=json_extract(c.value,'$.source')),''),
 COALESCE((SELECT json_extract(i.value,'$.ref') FROM json_each(s.layout_json,'$.instances') i WHERE json_extract(i.value,'$.instanceId')=json_extract(c.value,'$.target')),''),
 s.id, '[' || s.name || '] ',s.created_at,s.updated_at
FROM canvas_schemes s,json_each(s.layout_json,'$.connections') c WHERE s.execution='server';
UPDATE canvas_schemes SET layout_json=json_set(layout_json,'$.connections',json(COALESCE((
 SELECT json_group_array(json_set(c.value,'$.tunnelId',canvas_schemes.id || '.' || c.key))
 FROM json_each(canvas_schemes.layout_json,'$.connections') c),'[]'))) WHERE execution='server';
INSERT INTO canvas_tunnel_publications SELECT s.id,t.id FROM canvas_schemes s JOIN canvas_tunnels t ON t.inline_scheme_id=s.id WHERE s.published_at<>'';
-- Polymorphic object identity has no SQL FK; these triggers prevent dangling
-- ownership when the Owner uses the ordinary object deletion APIs.
CREATE TRIGGER canvas_tunnel_inbound_deleted AFTER DELETE ON inbounds BEGIN DELETE FROM canvas_tunnel_objects WHERE object_kind='inbound' AND object_id=OLD.id; END;
CREATE TRIGGER canvas_tunnel_outbound_deleted AFTER DELETE ON outbounds BEGIN DELETE FROM canvas_tunnel_objects WHERE object_kind='outbound' AND object_id=OLD.id; END;
CREATE TRIGGER canvas_tunnel_chain_deleted AFTER DELETE ON proxy_chains BEGIN DELETE FROM canvas_tunnel_objects WHERE object_kind='chain' AND object_id=OLD.id; END;
