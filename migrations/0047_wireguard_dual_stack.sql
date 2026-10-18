ALTER TABLE wireguard_addresses ADD COLUMN additional_address TEXT NOT NULL DEFAULT '';
ALTER TABLE relay_bindings ADD COLUMN additional_address TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX wireguard_additional_address ON wireguard_addresses(inbound_id,additional_address) WHERE additional_address<>'';
CREATE UNIQUE INDEX relay_additional_address ON relay_bindings(inbound_id,additional_address) WHERE additional_address<>'';
