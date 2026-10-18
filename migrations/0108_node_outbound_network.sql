-- Outbound network settings per node: the proxy core's outbound source address
-- and interface (capability A), plus the server-wide enforced source addresses
-- (capability B). An empty string means "not configured": an existing node keeps
-- its current behaviour and the Agent renders neither a socket option nor an
-- nftables rule for it.
ALTER TABLE nodes ADD COLUMN outbound_source_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN outbound_interface TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN host_source_ipv4 TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN host_source_ipv6 TEXT NOT NULL DEFAULT '';
