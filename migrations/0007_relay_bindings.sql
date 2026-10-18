CREATE TABLE relay_bindings (
 outbound_id TEXT PRIMARY KEY REFERENCES outbounds(id) ON DELETE CASCADE,
 inbound_id TEXT NOT NULL REFERENCES inbounds(id),
 credential_id TEXT NOT NULL REFERENCES relay_credentials(id),
 tunnel_address TEXT NOT NULL DEFAULT ''
);
