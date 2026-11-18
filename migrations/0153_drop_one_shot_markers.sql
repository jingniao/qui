-- v1.6.1: drop the markers and reports of one-shot conversions that a v1.6.0
-- panel finishes at startup (0124 canvas tunnels, 0131 canvas forwards, 0147
-- imported proxy identities, 0150 relay cleanup), and the unused app_metadata.
-- A marker that is not done means v1.6.0 never completed its startup; the
-- guard row then fails and the database stays as it was.
CREATE TEMP TABLE one_shot_guard (
    done INTEGER CONSTRAINT start_v1_6_0_once_before_upgrading CHECK (done = 1)
);
INSERT INTO one_shot_guard SELECT done FROM canvas_tunnel_reconcile;
INSERT INTO one_shot_guard SELECT done FROM canvas_forward_reconcile;
INSERT INTO one_shot_guard SELECT done FROM import_identity_reconcile;
INSERT INTO one_shot_guard SELECT completed FROM relay_cleanup_reconcile;
DROP TABLE one_shot_guard;
DROP TABLE app_metadata;
DROP TABLE canvas_tunnel_reconcile;
DROP TABLE canvas_forward_reconcile;
DROP TABLE tunnel_migration_report;
DROP TABLE import_identity_map;
DROP TABLE import_identity_reconcile;
DROP TABLE relay_cleanup_reconcile;
DELETE FROM system_settings WHERE key = 'import_identity_report';
