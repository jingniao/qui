package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
	"testing"
)

func TestRestoreLatencyTargetsCurrentSettingsAndOldArchive(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "pre0121"}[old], func(t *testing.T) {
			ctx := context.Background()
			f := listeningFixture(t, func(f *fixture) {
				queries := []string{"INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES('snapshot','snapshot','1.1.1.1',443,1)"}
				if old {
					rewindPostK4Fixture(t, f.source.Database)
					queries = append(queries, "DROP TRIGGER canvas_tunnel_inbound_deleted", "DROP TRIGGER canvas_tunnel_outbound_deleted", "DROP TRIGGER canvas_tunnel_chain_deleted", "DROP TABLE canvas_tunnel_objects", "DROP TABLE canvas_tunnel_publications", "DROP TABLE canvas_tunnels", "DROP TABLE canvas_tunnel_reconcile", "DELETE FROM schema_migrations WHERE version='0124_canvas_tunnels.sql'", "DROP TABLE node_metric_samples", "DROP TABLE node_tuning", "DELETE FROM schema_migrations WHERE version='0123_node_metrics_tuning.sql'")
					queries = append(queries, "DROP TRIGGER node_group_inbounds_insert", "DROP TRIGGER node_group_inbounds_delete", "DROP TRIGGER node_group_inbound_move", "DROP TABLE node_group_servers", "DROP TABLE node_group_inbounds", "DROP TABLE node_group_imports", "ALTER TABLE node_meta DROP COLUMN port_range_end", "ALTER TABLE node_meta DROP COLUMN port_range_start", "DELETE FROM schema_migrations WHERE version='0122_node_group_entries.sql'", "DROP TABLE node_latency_targets", "ALTER TABLE nodes DROP COLUMN latency_targets_custom", "DELETE FROM schema_migrations WHERE version='0121_node_latency_targets.sql'")
				}
				for _, q := range queries {
					if _, err := f.source.Database.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				"INSERT INTO probe_targets(id,label,host,port) VALUES('new','new','1.0.0.1',443)",
				"UPDATE nodes SET latency_targets_custom=1",
				"INSERT INTO node_latency_targets SELECT id,'new' FROM nodes",
			} {
				if _, err := f.source.Database.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			assertInt(t, restored, "SELECT latency_targets_custom FROM nodes WHERE id=?", 1, f.node.ID)
			assertInt(t, restored, "SELECT count(*) FROM node_latency_targets WHERE node_id=? AND target_id='new'", 1, f.node.ID)
			assertInt(t, restored, "SELECT scheduled FROM probe_targets WHERE id='new'", 0)
			assertInt(t, restored, "SELECT scheduled FROM probe_targets WHERE id='snapshot'", 1)
		})
	}
}
