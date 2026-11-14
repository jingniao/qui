package server

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
)

func importDeletePortalFixture(t *testing.T, count int, empty bool) (*Server, budgetHTTP, string, string, string) {
	t.Helper()
	app := testServer(t)
	owner := budgetOwner(t, app)
	content := "socks5://user:pass@example.com:1080#DeleteMe"
	if count == 2 {
		content += "\nsocks5://user:pass@example.org:1081#KeepMe"
	}
	src, _, err := app.importStore.Save(context.Background(), "", importer.Input{Name: "单独导入", Content: content, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var target, other string
	if err := app.Database.QueryRow("SELECT id FROM imported_proxies WHERE name='DeleteMe'").Scan(&target); err != nil {
		t.Fatal(err)
	}
	if count == 2 {
		if err := app.Database.QueryRow("SELECT id FROM imported_proxies WHERE name='KeepMe'").Scan(&other); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.Database.Exec("INSERT INTO node_groups(id,name,purpose,created_at) VALUES('delete-group','测试分组','subscription','2026-11-14T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{target, other} {
		if key == "" {
			continue
		}
		if _, err := app.Database.Exec("INSERT INTO node_group_imports(group_id,source_id,proxy_key) VALUES('delete-group',?,?)", src, "id:"+key); err != nil {
			t.Fatal(err)
		}
	}
	template := rules.Default()
	member := "@endpoint:import:" + src + ":id:" + target
	template.Groups[0].Members = []string{member}
	if !empty {
		template.Groups[0].Members = append(template.Groups[0].Members, rules.TargetDirect)
	}
	template.Groups[0].ExcludeEndpoints = []string{member}
	if other != "" {
		template.Groups[0].Members = append(template.Groups[0].Members, "@endpoint:import:"+src+":id:"+other)
		template.Groups[0].ExcludeEndpoints = append(template.Groups[0].ExcludeEndpoints, "@endpoint:import:"+src+":id:"+other)
	}
	if _, _, err := (&rules.Store{Database: app.Database}).Save(context.Background(), template, 0, "owner"); err != nil {
		t.Fatal(err)
	}
	return app, owner, src, target, other
}

func importDeletePortalCount(t *testing.T, app *Server, query string, args ...any) int {
	t.Helper()
	var count int
	if err := app.Database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPortalImportedProxyDeletionReferencesAndAudit(t *testing.T) {
	app, owner, src, target, other := importDeletePortalFixture(t, 2, false)
	path := "/api/v1/imported-proxies/" + target
	r := owner.call("GET", path+"/deletion-preview", nil)
	var preview importer.DeletionPreview
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &preview) != nil {
		t.Fatalf("preview: %d %s", r.Code, r.Body.String())
	}
	if preview.Node.ID != target || preview.Node.Name != "DeleteMe" || preview.Node.Protocol != "socks5" || preview.LastInSource || len(preview.Groups) != 1 || preview.Groups[0].ID != "delete-group" || preview.Groups[0].Name != "测试分组" || len(preview.ClientPolicies) != 1 || !preview.ClientPolicies[0].Active || len(preview.Canvases) != 0 {
		t.Fatalf("preview=%+v", preview)
	}
	request := importer.DeleteInput{ExpectedRevision: 1}
	r = owner.call("DELETE", path, request)
	if r.Code != 409 || !strings.Contains(r.Body.String(), `"code":"import_referenced"`) || !strings.Contains(r.Body.String(), "测试分组") {
		t.Fatalf("unconfirmed: %d %s", r.Code, r.Body.String())
	}
	noCSRF := owner
	noCSRF.csrf = ""
	request.RemoveReferences = true
	if r := noCSRF.call("DELETE", path, request); r.Code != 403 {
		t.Fatalf("csrf: %d %s", r.Code, r.Body.String())
	}
	var otherBefore string
	if err := app.Database.QueryRow("SELECT hex(payload_encrypted)||name||protocol||revision||fetched_at FROM imported_proxies WHERE id=?", other).Scan(&otherBefore); err != nil {
		t.Fatal(err)
	}
	r = owner.call("DELETE", path, request)
	if r.Code != 204 {
		t.Fatalf("delete: %d %s", r.Code, r.Body.String())
	}
	if importDeletePortalCount(t, app, "SELECT count(*) FROM imported_proxies WHERE id=?", target) != 0 || importDeletePortalCount(t, app, "SELECT count(*) FROM subscription_sources WHERE id=?", src) != 1 || importDeletePortalCount(t, app, "SELECT count(*) FROM node_group_imports WHERE group_id='delete-group' AND proxy_key=?", "id:"+other) != 1 {
		t.Fatal("incorrect remaining rows")
	}
	var otherAfter string
	if err := app.Database.QueryRow("SELECT hex(payload_encrypted)||name||protocol||revision||fetched_at FROM imported_proxies WHERE id=?", other).Scan(&otherAfter); err != nil {
		t.Fatal(err)
	}
	if otherAfter != otherBefore {
		t.Fatal("other node changed")
	}
	stored, err := (&rules.Store{Database: app.Database}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	otherMember := "@endpoint:import:" + src + ":id:" + other
	if !reflect.DeepEqual(stored.Template.Groups[0].Members, []string{rules.TargetDirect, otherMember}) || !reflect.DeepEqual(stored.Template.Groups[0].ExcludeEndpoints, []string{otherMember}) {
		t.Fatalf("policy=%+v", stored.Template.Groups[0])
	}
	if importDeletePortalCount(t, app, "SELECT count(*) FROM admin_operations WHERE action='删除导入节点' AND target=? AND result='成功'", target) != 1 {
		t.Fatal("missing delete audit")
	}
}

func TestPortalImportedProxyDeletionCanvasDraftAndPublished(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "draft", true: "published"}[published], func(t *testing.T) {
			app, owner, _, target, _ := importDeletePortalFixture(t, 1, false)
			layout := `{"instances":[{"instanceId":"hop","ref":"import:` + target + `","x":0,"y":0,"role":"exit"}],"connections":[]}`
			draft, snapshot := layout, ""
			if published {
				draft, snapshot = `{"instances":[],"connections":[]}`, layout
			}
			if _, err := app.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at) VALUES('delete-canvas','引用方案','client',?,?,'2026-11-14T00:00:00Z','2026-11-14T00:00:00Z')", draft, snapshot); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/imported-proxies/" + target
			r := owner.call("GET", path+"/deletion-preview", nil)
			var preview importer.DeletionPreview
			if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &preview) != nil || len(preview.Canvases) != 1 || preview.Canvases[0].SchemeID != "delete-canvas" || preview.Canvases[0].SchemeName != "引用方案" {
				t.Fatalf("preview: %d %s", r.Code, r.Body.String())
			}
			r = owner.call("DELETE", path, importer.DeleteInput{ExpectedRevision: 1, RemoveReferences: true})
			if r.Code != 409 || !strings.Contains(r.Body.String(), `"code":"import_in_use_by_canvas"`) || !strings.Contains(r.Body.String(), "引用方案") {
				t.Fatalf("delete: %d %s", r.Code, r.Body.String())
			}
			if importDeletePortalCount(t, app, "SELECT count(*) FROM imported_proxies WHERE id=?", target) != 1 || importDeletePortalCount(t, app, "SELECT count(*) FROM node_group_imports WHERE proxy_key=?", "id:"+target) != 1 {
				t.Fatal("blocked deletion changed rows")
			}
		})
	}
}

func TestPortalImportedProxyDeletionEmptyPolicyGroupRollback(t *testing.T) {
	app, owner, _, target, _ := importDeletePortalFixture(t, 1, true)
	r := owner.call("DELETE", "/api/v1/imported-proxies/"+target, importer.DeleteInput{ExpectedRevision: 1, RemoveReferences: true})
	if r.Code != 409 || !strings.Contains(r.Body.String(), "会被删空") || !strings.Contains(r.Body.String(), rules.Default().Groups[0].Name) {
		t.Fatalf("delete: %d %s", r.Code, r.Body.String())
	}
	if importDeletePortalCount(t, app, "SELECT count(*) FROM imported_proxies WHERE id=?", target) != 1 || importDeletePortalCount(t, app, "SELECT count(*) FROM node_group_imports WHERE proxy_key=?", "id:"+target) != 1 {
		t.Fatal("empty group failure did not roll back")
	}
}

func TestPortalImportedProxyDeletionTransactionRollback(t *testing.T) {
	for _, table := range []string{"imported_proxies", "subscription_sources"} {
		t.Run(table, func(t *testing.T) {
			app, owner, src, target, _ := importDeletePortalFixture(t, 1, false)
			store := &rules.Store{Database: app.Database}
			before, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			profilesBefore, err := store.Profiles(context.Background(), 1, 50)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.Database.Exec("CREATE TRIGGER import_delete_failure BEFORE DELETE ON " + table + " BEGIN SELECT RAISE(ABORT,'delete failure'); END"); err != nil {
				t.Fatal(err)
			}
			r := owner.call("DELETE", "/api/v1/imported-proxies/"+target, importer.DeleteInput{ExpectedRevision: 1, RemoveReferences: true})
			if r.Code != 500 {
				t.Fatalf("delete: %d %s", r.Code, r.Body.String())
			}
			if importDeletePortalCount(t, app, "SELECT count(*) FROM imported_proxies WHERE id=?", target) != 1 || importDeletePortalCount(t, app, "SELECT count(*) FROM subscription_sources WHERE id=?", src) != 1 || importDeletePortalCount(t, app, "SELECT count(*) FROM node_group_imports WHERE proxy_key=?", "id:"+target) != 1 {
				t.Fatal("failed transaction changed rows")
			}
			after, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			profilesAfter, err := store.Profiles(context.Background(), 1, 50)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(profilesBefore, profilesAfter) {
				t.Fatal("policy writes escaped rollback")
			}
			if importDeletePortalCount(t, app, "SELECT count(*) FROM admin_operations WHERE action='删除导入节点' AND target=?", target) != 0 {
				t.Fatal("failed deletion audited success")
			}
		})
	}
}
