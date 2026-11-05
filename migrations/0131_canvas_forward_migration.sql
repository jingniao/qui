-- Go transfers only unambiguous published canvas forwards in one transaction.
CREATE TABLE canvas_forward_reconcile (
    id INTEGER PRIMARY KEY CHECK(id=1),
    done INTEGER NOT NULL CHECK(done IN (0,1)),
    migrated_ids TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(migrated_ids) AND json_type(migrated_ids)='array')
);
INSERT INTO canvas_forward_reconcile(id,done) VALUES(1,0);
CREATE TABLE tunnel_migration_report (
    tunnel_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    reason TEXT NOT NULL
);
