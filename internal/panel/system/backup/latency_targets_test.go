package backup

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNodeLatencyTargetsArchiveAndPre0121(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "pre0121"}[old], func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			ctx := context.Background()
			queries := []string{
				"INSERT INTO probe_targets(id,label,host,port) VALUES('custom','custom','1.1.1.1',443)",
				"UPDATE nodes SET latency_targets_custom=1",
				"INSERT INTO node_latency_targets SELECT id,'custom' FROM nodes",
			}
			if old {
				rewindPostK4Fixture(t, source.Database)
				queries = append(queries, "DROP TRIGGER canvas_tunnel_inbound_deleted", "DROP TRIGGER canvas_tunnel_outbound_deleted", "DROP TRIGGER canvas_tunnel_chain_deleted", "DROP TABLE canvas_tunnel_objects", "DROP TABLE canvas_tunnel_publications", "DROP TABLE canvas_tunnels", "DROP TABLE canvas_tunnel_reconcile", "DELETE FROM schema_migrations WHERE version='0124_canvas_tunnels.sql'", "DROP TABLE node_metric_samples", "DROP TABLE node_tuning", "DELETE FROM schema_migrations WHERE version='0123_node_metrics_tuning.sql'")
				queries = append(queries, "DROP TRIGGER node_group_inbounds_insert", "DROP TRIGGER node_group_inbounds_delete", "DROP TRIGGER node_group_inbound_move", "DROP TABLE node_group_servers", "DROP TABLE node_group_inbounds", "DROP TABLE node_group_imports", "ALTER TABLE node_meta DROP COLUMN port_range_end", "ALTER TABLE node_meta DROP COLUMN port_range_start", "DELETE FROM schema_migrations WHERE version='0122_node_group_entries.sql'", "DROP TABLE node_latency_targets", "ALTER TABLE nodes DROP COLUMN latency_targets_custom", "DELETE FROM schema_migrations WHERE version='0121_node_latency_targets.sql'")
			}
			for _, q := range queries {
				if _, err := source.Database.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateNodeLatencyTargets(ctx, source.Database); err != nil {
				t.Fatal(err)
			}
			result, err := source.Create(ctx, directory, "latency_targets_archive", "owner", testPassword)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
				database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
				if err != nil {
					return err
				}
				defer database.Close()
				inventory, err := ReadInventory(ctx, database)
				if err != nil {
					return err
				}
				if old && inventory.Totals.NodeLatencyTargets != 0 || !old && inventory.Totals.NodeLatencyTargets == 0 {
					t.Fatal(inventory.Totals)
				}
				if !old {
					var count int
					if err = database.QueryRow("SELECT count(*) FROM nodes WHERE latency_targets_custom=1").Scan(&count); err != nil || count == 0 {
						t.Fatal(count, err)
					}
				}
				return validateNodeLatencyTargets(ctx, database)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !old {
				if _, err = source.Database.Exec("DROP TABLE node_latency_targets"); err != nil {
					t.Fatal(err)
				}
				if validateNodeLatencyTargets(ctx, source.Database) == nil {
					t.Fatal("missing configuration accepted")
				}
			}
		})
	}
}
