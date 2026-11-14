package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/migrations"
)

func TestPrepareDatabasePreservesCanvasEntryBindings(t *testing.T) {
	ctx := context.Background()
	var archived map[string][][]any
	f := listeningFixture(t, func(f *fixture) {
		r := &proxy.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		scheme, err := r.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "archived bindings", Execution: "server"})
		if err != nil {
			t.Fatal(err)
		}
		for index, state := range []string{"applied", "detached"} {
			inbound, err := r.SaveInbound(ctx, "", proxy.InboundInput{Name: state, NodeID: f.node.ID, Protocol: "http", Listen: "127.0.0.1", Port: 18080 + index, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			binding := proxy.CanvasEntryBindingRecord{ID: state, SchemeID: &scheme.ID, InstanceID: state, InboundID: inbound.ID, Mode: "create", AppliedDefaultOutbound: "direct", State: state}
			fields := map[string]string{}
			for _, key := range []string{"名称", "协议", "监听地址", "端口", "核心", "协议设置", "启用", "订阅可见", "允许用户"} {
				fields[key] = strings.Repeat("a", 64)
			}
			data, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			config, groups := string(data), `["historical-group"]`
			binding.CreationConfigJSON, binding.CreationGroupsJSON = &config, &groups
			if state == "detached" {
				binding.SchemeID = nil
				binding.Mode = "reuse"
				binding.OriginalDefaultOutbound = "missing-original-outbound"
			}
			if err := r.InsertCanvasEntryBinding(ctx, binding); err != nil {
				t.Fatal(err)
			}
		}
		archived = canvasRecoveryRows(t, f.source.Database, "canvas_entry_bindings", "canvas_schemes", "inbounds")
	})
	// Diverge live configuration so the test distinguishes archive restoration
	// from retaining current bindings, schemes and listeners.
	for _, query := range []string{
		"UPDATE canvas_entry_bindings SET applied_default_outbound='block'",
		"UPDATE canvas_schemes SET name='current changed scheme'",
		"UPDATE inbounds SET default_outbound='block'",
	} {
		if _, err := f.source.Database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	coordination := canvasRecoveryRows(t, f.source.Database, "config_revisions", "deployment_batches", "deployment_batch_items", "deployment_targets", "deployment_commands", "node_runtime_state")
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got := canvasRecoveryRows(t, restored, "canvas_entry_bindings", "canvas_schemes", "inbounds"); !reflect.DeepEqual(got, archived) {
		t.Fatalf("archive configuration changed: got %#v, want %#v", got, archived)
	}
	if got := canvasRecoveryRows(t, restored, "config_revisions", "deployment_batches", "deployment_batch_items", "deployment_targets", "deployment_commands", "node_runtime_state"); !reflect.DeepEqual(got, coordination) {
		t.Fatalf("restore created or changed deployment execution: got %#v, want %#v", got, coordination)
	}
}

func TestPrepareDatabasePre0146ArchiveHasNoCanvasEntryBindings(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		rewindPost0143(t, f.source.Database)
	})
	if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM schema_migrations WHERE version='0146_canvas_entry_bindings.sql'", 1)
	assertInt(t, restored, "SELECT count(*) FROM canvas_entry_bindings", 0)
}

func rewindCanvasEntryBaseline(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, q := range []string{"ALTER TABLE canvas_entry_bindings DROP COLUMN creation_config_json", "ALTER TABLE canvas_entry_bindings DROP COLUMN creation_groups_json", "DELETE FROM schema_migrations WHERE version='0148_canvas_entry_baseline.sql'"} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrepareDatabasePre0148PreservesUnknownBaseline(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		r := &proxy.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		in, err := r.SaveInbound(ctx, "", proxy.InboundInput{Name: "historical", NodeID: f.node.ID, Protocol: "http", Listen: "127.0.0.1", Port: 18081, Enabled: true, DefaultOutbound: "direct"})
		if err != nil {
			t.Fatal(err)
		}
		if err = r.InsertCanvasEntryBinding(ctx, proxy.CanvasEntryBindingRecord{ID: "old-binding", InstanceID: "entry", InboundID: in.ID, Mode: "create", State: "detached", AppliedDefaultOutbound: "direct"}); err != nil {
			t.Fatal(err)
		}
		rewindCanvasEntryBaseline(t, f.source.Database)
	})
	if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM canvas_entry_bindings WHERE id='old-binding' AND creation_config_json IS NULL AND creation_groups_json IS NULL", 1)
}

// Compare every persisted field, including sealed settings, ownership,
// timestamps and request identifiers, without exposing decrypted materials.
func canvasRecoveryRows(t *testing.T, database *sql.DB, tables ...string) map[string][][]any {
	t.Helper()
	result := make(map[string][][]any, len(tables))
	for _, table := range tables {
		rows, err := database.Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		result[table] = nil
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			result[table] = append(result[table], values)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return result
}
