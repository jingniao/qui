ALTER TABLE nodes ADD COLUMN public_addresses_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE nodes ADD COLUMN public_ip_selected TEXT NOT NULL DEFAULT '';
UPDATE nodes SET public_ip_selected = public_ip WHERE public_ip_manual = 1 AND public_ip <> '';
