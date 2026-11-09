-- The client address of a node is its connect_host when the Owner set one (a
-- domain or IP), otherwise its public_ip. public_ip can now be detected from
-- the Agent's heartbeat, so this column records whether the stored value was
-- typed by the Owner and must never be overwritten. Every value that exists
-- today was typed by hand: nothing else ever wrote it.
ALTER TABLE nodes ADD COLUMN public_ip_manual INTEGER NOT NULL DEFAULT 0 CHECK(public_ip_manual IN (0,1));
ALTER TABLE nodes ADD COLUMN connect_host TEXT NOT NULL DEFAULT '';
UPDATE nodes SET public_ip_manual=1 WHERE public_ip<>'';
