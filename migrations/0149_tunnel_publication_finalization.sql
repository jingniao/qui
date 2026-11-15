-- The public node IDs reserve the captured set; the business before/after images
-- and TLS material live only in the Vault-sealed JSON envelope member.
ALTER TABLE deployment_batches ADD COLUMN tunnel_publication_json TEXT;
ALTER TABLE deployment_batches ADD COLUMN tunnel_finalized INTEGER NOT NULL DEFAULT 1
    CHECK(tunnel_finalized IN (0,1));
