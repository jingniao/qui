package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
	"testing"
)

func TestPre0124CanvasArchiveMigratesAndAdoptsExactPublication(t *testing.T) {
	ctx := context.Background()
	var objectID = "legacy-forward"
	f := listeningFixture(t, func(f *fixture) {
		rewindPostK4Fixture(t, f.source.Database)
		port, target := 30000, 443
		layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "a", Ref: "server:" + f.node.ID, Role: "entry"}, {InstanceID: "b", Ref: "server:" + f.node.ID, Role: "exit"}}, Connections: []proxy.CanvasConnection{{ID: "edge", Source: "a", Target: "b", Type: "port_forward", ListenPort: &port, TargetHost: "example.com", TargetPort: &target, SupportTCP: true}}}
		raw, err := json.Marshal(layout)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,created_at,updated_at,published_at) VALUES('legacy-scheme','legacy','server',?,'2026-10-29T00:00:00Z','2026-10-29T00:00:00Z','2026-10-29T00:00:00Z')", string(raw)); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte("edge:" + f.node.ID + ":" + f.node.ID))
		name := fmt.Sprintf("[legacy] forward %x 中转入口", digest[:8])
		sealed, err := f.source.ConfigurationVault.Seal([]byte(`{"forward":{"host":"example.com","port":443,"network":"tcp"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?,?,?,'forward','0.0.0.0',30000,1,0,0,'direct','[]',?,'2026-10-29T00:00:00Z')", objectID, name, f.node.ID, sealed); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{"DROP TRIGGER canvas_tunnel_inbound_deleted", "DROP TRIGGER canvas_tunnel_outbound_deleted", "DROP TRIGGER canvas_tunnel_chain_deleted", "DROP TABLE canvas_tunnel_objects", "DROP TABLE canvas_tunnel_publications", "DROP TABLE canvas_tunnels", "DROP TABLE canvas_tunnel_reconcile", "DELETE FROM schema_migrations WHERE version='0124_canvas_tunnels.sql'"} {
			if _, err := f.source.Database.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	})
	// The archived snapshot stays pre-0124; the currently running panel has
	// upgraded its own database before starting an offline restore.
	if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	if err := (&proxy.Repository{Database: f.source.Database}).ReconcileCanvasTunnels(ctx); err != nil {
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
	repository := &proxy.Repository{Database: restored}
	scheme, err := repository.CanvasScheme(ctx, "legacy-scheme")
	if err != nil || len(scheme.Layout.Connections) != 1 || scheme.Layout.Connections[0].TunnelID == "" {
		t.Fatal(scheme, err)
	}
	var owner string
	if err := restored.QueryRow("SELECT tunnel_id FROM canvas_tunnel_objects WHERE object_kind='inbound' AND object_id=?", objectID).Scan(&owner); err != nil || owner != scheme.Layout.Connections[0].TunnelID {
		t.Fatal(owner, err)
	}
	if err := repository.ReconcileCanvasTunnels(ctx); err != nil {
		t.Fatal(err)
	}
	if err := proxy.ValidateCanvasTunnels(ctx, restored); err != nil {
		t.Fatal(err)
	}
}
