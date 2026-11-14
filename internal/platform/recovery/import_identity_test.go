package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/testutil/dbtest"
)

func reconcileImportGroupReferences(ctx context.Context, tx *sql.Tx, resolve func(string, string) ([]string, bool), _ map[string]string) ([]string, error) {
	return node.ReconcileImportIdentitiesTx(ctx, tx, resolve)
}

func TestImportIdentityRestoreMixedSnapshots(t *testing.T) {
	for _, archivedDone := range []bool{false, true} {
		t.Run(map[bool]string{false: "old_archive_current_done", true: "done_archive_current_pending"}[archivedDone], func(t *testing.T) {
			ctx := context.Background()
			var src, oldID, archivedID, profileID string
			f := listeningFixture(t, func(f *fixture) {
				store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
				var err error
				src, _, err = store.Save(ctx, "", importer.Input{Name: "mixed", Content: "http://user:secret@example.com:8080#Original", Interval: 60})
				if err != nil {
					t.Fatal(err)
				}
				if err = f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&oldID); err != nil {
					t.Fatal(err)
				}
				archivedID = oldID
				if archivedDone {
					if err = store.ReconcileImportedIdentities(ctx); err != nil {
						t.Fatal(err)
					}
					if err = f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&archivedID); err != nil {
						t.Fatal(err)
					}
				}
				template := rules.Default()
				member := "@endpoint:import:" + src + ":Original|http"
				if archivedDone {
					member = "@endpoint:import:" + src + ":id:" + archivedID
				}
				template.Groups[0].Members = []string{member}
				if err := (&rules.Store{Database: f.source.Database}).Initialize(ctx); err != nil {
					t.Fatal(err)
				}
				profile, _, err := (&rules.Store{Database: f.source.Database}).WriteProfile(ctx, "", rules.ProfileInput{Name: "import identity", Template: template, UIMetadata: json.RawMessage(`{}`)}, false, "owner")
				if err != nil {
					t.Fatal(err)
				}
				profileID = profile.Profile.ID
				layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "import", Ref: "import:" + archivedID, Role: "entry"}}}
				raw, err := json.Marshal(layout)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.source.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at) VALUES('mixed','mixed','client',?,?,'2026-11-13T01:00:00Z','2026-11-13T01:00:00Z')", string(raw), string(raw)); err != nil {
					t.Fatal(err)
				}
			})
			store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
			if archivedDone {
				for _, q := range []string{"UPDATE imported_proxies SET id=(SELECT old_proxy_id FROM import_identity_map WHERE new_proxy_id=imported_proxies.id)", "DELETE FROM import_identity_map", "UPDATE import_identity_reconcile SET done=0"} {
					if _, err := f.source.Database.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
				raw := `{"instances":[{"instanceId":"import","ref":"import:` + oldID + `","x":0,"y":0,"role":"entry"}],"connections":[]}`
				if _, err := f.source.Database.Exec("UPDATE canvas_schemes SET layout_json=?,client_published_layout=?", raw, raw); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := store.ReconcileImportedIdentities(ctx); err != nil {
					t.Fatal(err)
				}
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
			var restoredID, draft, published, template string
			if err := restored.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&restoredID); err != nil {
				t.Fatal(err)
			}
			if err := restored.QueryRow("SELECT layout_json,client_published_layout FROM canvas_schemes WHERE id='mixed'").Scan(&draft, &published); err != nil {
				t.Fatal(err)
			}
			for _, raw := range []string{draft, published} {
				var layout proxy.CanvasLayout
				if err := json.Unmarshal([]byte(raw), &layout); err != nil {
					t.Fatal(err)
				}
				if layout.Instances[0].Ref != "import:"+restoredID {
					t.Fatalf("wrong restored canvas: %s", raw)
				}
			}
			if err := restored.QueryRow("SELECT template_json FROM client_policy_profiles WHERE id=?", profileID).Scan(&template); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(template, "@endpoint:import:"+src+":id:"+restoredID) {
				t.Fatalf("wrong restored template: %s", template)
			}
			before := restoredID
			if err := (&importer.Store{Database: restored, Vault: f.source.ConfigurationVault}).ReconcileImportedIdentities(ctx, func(context.Context, *sql.Tx, func(string, string) ([]string, bool), map[string]string) ([]string, error) {
				t.Fatal("done restore repeated conversion")
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := restored.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&restoredID); err != nil || restoredID != before {
				t.Fatalf("repeated conversion %s %v", restoredID, err)
			}
		})
	}
}

func TestImportIdentityRestoreVersions(t *testing.T) {
	for _, version := range []string{"pre0147", "pending", "done"} {
		t.Run(version, func(t *testing.T) {
			ctx := context.Background()
			var sourceID, originalID, groupID, originalMapping, originalReport string
			f := listeningFixture(t, func(f *fixture) {
				store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
				var err error
				sourceID, _, err = store.Save(ctx, "", importer.Input{Name: "identity fixture", Content: "http://user:secret@example.com:8080#Original", Interval: 60})
				if err != nil {
					t.Fatal(err)
				}
				if err = f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", sourceID).Scan(&originalID); err != nil {
					t.Fatal(err)
				}
				group, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Imports", Purpose: "subscription", Enabled: true, SubscriptionVisible: true})
				if err != nil {
					t.Fatal(err)
				}
				groupID = group.ID
				if _, err = f.source.Database.Exec("INSERT INTO node_group_imports VALUES(?,?,?)", groupID, sourceID, ""); err != nil {
					t.Fatal(err)
				}
				if err = f.users.Grant(ctx, f.user.ID, groupID, false); err != nil {
					t.Fatal(err)
				}
				if version == "done" {
					if err = store.ReconcileImportedIdentities(ctx, reconcileImportGroupReferences); err != nil {
						t.Fatal(err)
					}
					if err = f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", sourceID).Scan(&originalID); err != nil {
						t.Fatal(err)
					}
					if _, err = f.source.Database.Exec("UPDATE imported_proxies SET revision=7 WHERE id=?", originalID); err != nil {
						t.Fatal(err)
					}
					if err = f.source.Database.QueryRow("SELECT json_array(source_id,old_proxy_id,old_proxy_key,new_proxy_id) FROM import_identity_map").Scan(&originalMapping); err != nil {
						t.Fatal(err)
					}
					if err = f.source.Database.QueryRow("SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&originalReport); err != nil {
						t.Fatal(err)
					}
				} else if version == "pre0147" {
					rewindCanvasEntryBaseline(t, f.source.Database)
					for _, q := range []string{"DROP TABLE import_identity_map", "DROP TABLE import_identity_reconcile", "ALTER TABLE imported_proxies DROP COLUMN revision", "DELETE FROM schema_migrations WHERE version='0147_imported_proxy_identity.sql'"} {
						if _, err = f.source.Database.Exec(q); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
			candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			assertInt(t, restored, "SELECT done FROM import_identity_reconcile", 1)
			assertInt(t, restored, "SELECT count(*) FROM import_identity_map", 1)
			assertInt(t, restored, "SELECT count(*) FROM node_group_imports WHERE proxy_key=''", 0)
			var restoredID string
			if err = restored.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", sourceID).Scan(&restoredID); err != nil {
				t.Fatal(err)
			}
			if version == "done" {
				if restoredID != originalID {
					t.Fatalf("stable identity changed: %s -> %s", originalID, restoredID)
				}
				assertInt(t, restored, "SELECT revision FROM imported_proxies WHERE id=?", 7, restoredID)
				var mapping, report string
				if err = restored.QueryRow("SELECT json_array(source_id,old_proxy_id,old_proxy_key,new_proxy_id) FROM import_identity_map").Scan(&mapping); err != nil {
					t.Fatal(err)
				}
				if err = restored.QueryRow("SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&report); err != nil {
					t.Fatal(err)
				}
				if mapping != originalMapping || report != originalReport {
					t.Fatalf("completed identity state changed: %s %s", mapping, report)
				}
			} else if restoredID == originalID {
				t.Fatal("legacy identity was not converted")
			}
			endpoints, _, err := (&importer.Store{Database: restored, Vault: f.source.ConfigurationVault}).Proxies(ctx, source.Viewer{UserID: f.user.ID, AllowedProtocols: []string{"http"}})
			if err != nil || len(endpoints) != 1 || endpoints[0].Origin.Ref != restoredID {
				t.Fatalf("restored authorization: %+v, %v", endpoints, err)
			}
		})
	}
}

func TestImportIdentityRestoreDoesNotReviveWholeSourceMembership(t *testing.T) {
	ctx := context.Background()
	var groupID string
	f := listeningFixture(t, func(f *fixture) {
		store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		src, _, err := store.Save(ctx, "", importer.Input{Name: "revoked import", Content: "http://user:secret@example.com:8080#Original", Interval: 60, UserIDs: []string{f.user.ID}})
		if err != nil {
			t.Fatal(err)
		}
		group, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Revoked import", Purpose: "subscription", Enabled: true, SubscriptionVisible: true})
		if err != nil {
			t.Fatal(err)
		}
		groupID = group.ID
		if _, err = f.source.Database.Exec("INSERT INTO node_group_imports VALUES(?,?,?)", groupID, src, ""); err != nil {
			t.Fatal(err)
		}
		if err = f.users.Grant(ctx, f.user.ID, groupID, false); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec("DELETE FROM node_group_imports WHERE group_id=?", groupID); err != nil {
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
	assertInt(t, restored, "SELECT count(*) FROM node_group_imports WHERE group_id=?", 0, groupID)
	endpoints, _, err := (&importer.Store{Database: restored, Vault: f.source.ConfigurationVault}).Proxies(ctx, source.Viewer{UserID: f.user.ID, AllowedProtocols: []string{"http"}})
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("revoked imports restored: %+v %v", endpoints, err)
	}
}

func TestImportIdentityRestoreRetainsUnmappedArchivedCanvas(t *testing.T) {
	ctx := context.Background()
	var src, rowID string
	f := listeningFixture(t, func(f *fixture) {
		store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		var err error
		src, _, err = store.Save(ctx, "", importer.Input{Name: "removed", Content: "http://user:secret@example.com:8080#Original", Interval: 60})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "import", Ref: "import:" + rowID, Role: "entry"}}}
		raw, err := json.Marshal(layout)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.source.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,created_at,updated_at) VALUES('unmapped','unmapped','client',?,'2026-11-13T01:00:00Z','2026-11-13T01:00:00Z')", string(raw)); err != nil {
			t.Fatal(err)
		}
	})
	if err := (&importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}).Delete(ctx, src); err != nil {
		t.Fatal(err)
	}
	// The node was deleted after the backup: the archived drawing keeps its
	// dangling reference and the conversion reports it instead of failing.
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var raw, report string
	if err := restored.QueryRow("SELECT layout_json FROM canvas_schemes WHERE id='unmapped'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "import:"+rowID) {
		t.Fatalf("dangling reference not retained: %s", raw)
	}
	if err := restored.QueryRow("SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "import:"+rowID) {
		t.Fatalf("dangling reference not reported: %s", report)
	}
}

func TestImportIdentityRestorePreservesMissingURLCanvas(t *testing.T) {
	ctx := context.Background()
	var src, rowID string
	f := listeningFixture(t, func(f *fixture) {
		store := &importer.Store{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		var err error
		src, _, err = store.Save(ctx, "", importer.Input{Name: "airport", Content: "http://user:secret@example.com:8080#Original", Interval: 60})
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := f.source.ConfigurationVault.Seal([]byte("https://example.com/subscription"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.source.Database.Exec("UPDATE subscription_sources SET url_encrypted=? WHERE id=?", sealed, src); err != nil {
			t.Fatal(err)
		}
		if err = f.source.Database.QueryRow("SELECT id FROM imported_proxies WHERE source_id=?", src).Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "import", Ref: "import:" + rowID, Role: "entry"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.source.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,created_at,updated_at) VALUES('url','url','client',?,'2026-11-13T01:00:00Z','2026-11-13T01:00:00Z')", string(raw)); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec("DELETE FROM imported_proxies WHERE source_id=?", src); err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Database.Exec("UPDATE import_identity_reconcile SET done=1"); err != nil {
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
	var raw string
	if err := restored.QueryRow("SELECT layout_json FROM canvas_schemes WHERE id='url'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var layout proxy.CanvasLayout
	if err := json.Unmarshal([]byte(raw), &layout); err != nil {
		t.Fatal(err)
	}
	if layout.Instances[0].Ref != "import:"+rowID {
		t.Fatalf("URL reference changed: %s", raw)
	}
}
