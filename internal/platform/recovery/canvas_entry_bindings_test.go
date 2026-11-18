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

	"proxycontrol/internal/testutil/dbtest"
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
