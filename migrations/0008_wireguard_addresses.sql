CREATE TABLE wireguard_addresses (
 inbound_id TEXT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 address TEXT NOT NULL,
 PRIMARY KEY(inbound_id,device_id), UNIQUE(inbound_id,address)
);
CREATE UNIQUE INDEX relay_tunnel_address ON relay_bindings(inbound_id,tunnel_address) WHERE tunnel_address<>'';
