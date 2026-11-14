-- Go reconciles encrypted content identities and references in one transaction.
CREATE TABLE import_identity_reconcile (
    id INTEGER PRIMARY KEY CHECK(id=1),
    done INTEGER NOT NULL CHECK(done IN (0,1))
);
INSERT INTO import_identity_reconcile(id,done) VALUES(1,0);
CREATE TABLE import_identity_map (
    source_id TEXT NOT NULL,
    old_proxy_id TEXT NOT NULL,
    old_proxy_key TEXT NOT NULL,
    new_proxy_id TEXT NOT NULL UNIQUE,
    PRIMARY KEY(source_id,old_proxy_id)
);
ALTER TABLE imported_proxies ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
