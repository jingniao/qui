CREATE TABLE subscription_route_dependencies (
 endpoint_ref TEXT PRIMARY KEY,
 owner_node_id TEXT NOT NULL,
 publication_id TEXT NOT NULL,
 source_digest TEXT NOT NULL,
 node_ids_json TEXT NOT NULL CHECK(json_valid(node_ids_json) AND json_type(node_ids_json)='array'),
 path_node_ids_json TEXT NOT NULL CHECK(json_valid(path_node_ids_json) AND json_type(path_node_ids_json)='array'),
 target_inbound_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX subscription_route_dependencies_owner ON subscription_route_dependencies(owner_node_id);
