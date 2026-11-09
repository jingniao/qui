-- The panel-issued certificate store (v1.4.7 section 4, phase D1). One row is
-- one certificate request: the names it covers, where issuance stands, the
-- vault-sealed certificate and key once issued, and when the next attempt or
-- renewal is due. The panel talks to Cloudflare and to the ACME server itself,
-- so nothing here is sent to an Agent until phase D2 references it from an
-- inbound.
--
-- encrypted_material is sealed JSON {certificate_pem, private_key_pem} and stays
-- NULL until the first successful issuance, which is why the material column is
-- nullable while every status column has an empty-string default.
CREATE TABLE certificates (
    id TEXT PRIMARY KEY,
    domains_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending','issuing','valid','failed')),
    encrypted_material BLOB,
    not_before TEXT NOT NULL DEFAULT '',
    not_after TEXT NOT NULL DEFAULT '',
    last_error_code TEXT NOT NULL DEFAULT '',
    last_attempt_at TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
    auto_dns INTEGER NOT NULL DEFAULT 0 CHECK(auto_dns IN (0,1)),
    dns_node_id TEXT REFERENCES nodes(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX certificates_status ON certificates(status);

-- Phase D2 lets an inbound point at a stored certificate instead of embedding
-- it. SQLite refuses a REFERENCES clause on a new column unless the default is
-- NULL, which is also the meaning here: an inbound without a certificate row.
ALTER TABLE inbounds ADD COLUMN certificate_id TEXT REFERENCES certificates(id);
CREATE INDEX inbounds_certificate ON inbounds(certificate_id);
