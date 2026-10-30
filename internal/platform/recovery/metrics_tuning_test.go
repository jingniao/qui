package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
	"testing"
	"time"
)

func TestK2bRestorePreservesCurrentTuningWithoutResend(t *testing.T) {
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	f := listeningFixture(t, func(f *fixture) {
		if _, err := f.source.Database.Exec(`INSERT INTO node_tuning SELECT id,'{"net.core.somaxconn":"128"}','snapshot','pending',NULL,?,? FROM nodes`, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec(`UPDATE node_tuning SET desired_json='{"net.core.somaxconn":"256"}',request_id='current'; INSERT INTO node_metric_samples(node_id,at,cpu_percent) SELECT id,?,0 FROM nodes`, stamp); err != nil {
		t.Fatal(err)
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
	assertInt(t, restored, "SELECT count(*) FROM node_tuning WHERE request_id='current' AND status='unconfirmed' AND desired_json=?", 1, `{"net.core.somaxconn":"256"}`)
	assertInt(t, restored, "SELECT count(*) FROM node_metric_samples", 0)
}

func TestK2bRestoreDoesNotReviveSnapshotOnlyTuning(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := f.source.Database.Exec(`INSERT INTO node_tuning SELECT id,'{}','snapshot','pending',NULL,?,? FROM nodes`, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec("DELETE FROM node_tuning"); err != nil {
		t.Fatal(err)
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
	assertInt(t, restored, "SELECT count(*) FROM node_tuning", 0)
}

func TestK2bRestorePre0123KeepsCurrentAppliedTuning(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		rewindPostK4Fixture(t, f.source.Database)
		for _, q := range []string{"DROP TRIGGER canvas_tunnel_inbound_deleted", "DROP TRIGGER canvas_tunnel_outbound_deleted", "DROP TRIGGER canvas_tunnel_chain_deleted", "DROP TABLE canvas_tunnel_objects", "DROP TABLE canvas_tunnel_publications", "DROP TABLE canvas_tunnels", "DROP TABLE canvas_tunnel_reconcile", "DELETE FROM schema_migrations WHERE version='0124_canvas_tunnels.sql'", "DROP TABLE node_metric_samples", "DROP TABLE node_tuning", "DELETE FROM schema_migrations WHERE version='0123_node_metrics_tuning.sql'"} {
			if _, err := f.source.Database.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.source.Database.Exec(`INSERT INTO node_tuning SELECT id,'{"net.core.somaxconn":"256"}','current','applied','{"requestId":"current","status":"applied","keys":[{"key":"net.core.somaxconn","value":"256","applied":true}]}',?,? FROM nodes`, stamp, stamp); err != nil {
		t.Fatal(err)
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
	assertInt(t, restored, "SELECT count(*) FROM node_tuning WHERE request_id='current' AND status='applied' AND result_json IS NOT NULL", 1)
}
