-- W05 first recovered root: bind the durable publication to the exact prepared
-- historical restore plan/archive/source and the independent original-Owner
-- historical coordinator approval. Appended after 0092; the 0092 table is not
-- rewritten. The new self-signed current root is stored in an independent
-- protected current-root file (node-ca-recovered-base.enc), never by
-- overwriting the retained ca.crt/ca.key.
ALTER TABLE node_ca_recovered_root ADD COLUMN plan_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE node_ca_recovered_root ADD COLUMN archive_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE node_ca_recovered_root ADD COLUMN source_id TEXT NOT NULL DEFAULT '';
ALTER TABLE node_ca_recovered_root ADD COLUMN historical_approved_at TEXT NOT NULL DEFAULT '';
