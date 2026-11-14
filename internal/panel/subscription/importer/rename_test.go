package importer

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
)

func renameRequest(s *Store, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", key)
	w := httptest.NewRecorder()
	(&Handler{Store: s}).rename(w, r)
	return w
}

func renameFixture(t *testing.T) (*Store, string) {
	t.Helper()
	s, _, _ := storeFixture(t)
	_, _, err := s.Save(context.Background(), "", Input{Name: "manual", Content: identityContent, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err := s.Database.QueryRow("SELECT id FROM imported_proxies").Scan(&key); err != nil {
		t.Fatal(err)
	}
	return s, key
}

func renamePayload(t *testing.T, s *Store, key string) (proxy.Proxy, string, int, string) {
	t.Helper()
	var sealed []byte
	var name, stamp string
	var revision int
	if err := s.Database.QueryRow("SELECT name,revision,payload_encrypted,fetched_at FROM imported_proxies WHERE id=?", key).Scan(&name, &revision, &sealed, &stamp); err != nil {
		t.Fatal(err)
	}
	raw, err := s.Vault.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var p proxy.Proxy
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p, name, revision, stamp
}

func TestRenameImportedProxyAtomic(t *testing.T) {
	s, key := renameFixture(t)
	before, _, _, stamp := renamePayload(t, s, key)
	w := renameRequest(s, key, `{"name":" 新名称 | : ","expectedRevision":1}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	after, name, revision, newStamp := renamePayload(t, s, key)
	if name != "新名称 | :" || after.Name != name || revision != 2 || stamp != newStamp {
		t.Fatalf("name=%q payload=%q revision=%d stamp=%q", name, after.Name, revision, newStamp)
	}
	before.Name = name
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rename changed connection fields or credentials")
	}
	items, err := s.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(items[0])
	if strings.TrimSpace(w.Body.String()) != string(expected) {
		t.Fatalf("response=%s list=%s", w.Body.String(), expected)
	}
}

func TestRenameImportedProxyValidationAndStale(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"empty", `{"name":"  ","expectedRevision":1}`, 400, "import_invalid_name"},
		{"long", `{"name":"` + strings.Repeat("界", 67) + `","expectedRevision":1}`, 400, "import_invalid_name"},
		{"control", `{"name":"a\u0000b","expectedRevision":1}`, 400, "import_invalid_name"},
		{"stale", `{"name":"new","expectedRevision":2}`, 409, "stale"},
		{"unknown", `{"name":"new","expectedRevision":1,"port":80}`, 400, "invalid_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, key := renameFixture(t)
			w := renameRequest(s, key, tc.body)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			p, name, revision, _ := renamePayload(t, s, key)
			if name != "Original" || p.Name != name || revision != 1 {
				t.Fatal("rejected rename changed state")
			}
		})
	}
}

func TestRenameImportedProxyURLRejected(t *testing.T) {
	s, key := renameFixture(t)
	sealed, err := s.Vault.Seal([]byte("https://example.com/sub"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("UPDATE subscription_sources SET url_encrypted=?", sealed); err != nil {
		t.Fatal(err)
	}
	w := renameRequest(s, key, `{"name":"new","expectedRevision":1}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"import_rename_unsupported"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestRenameImportedProxyNameByteBoundary(t *testing.T) {
	s, key := renameFixture(t)
	name := strings.Repeat("界", 66) + "ab"
	body, err := json.Marshal(RenameInput{Name: name, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	w := renameRequest(s, key, string(body))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestRenameImportedProxyUnreadableSourceKeepsState(t *testing.T) {
	s, key := renameFixture(t)
	if _, err := s.Database.Exec("UPDATE subscription_sources SET url_encrypted=x'00'"); err != nil {
		t.Fatal(err)
	}
	w := renameRequest(s, key, `{"name":"new","expectedRevision":1}`)
	if w.Code != 500 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	p, name, revision, _ := renamePayload(t, s, key)
	if p.Name != "Original" || name != p.Name || revision != 1 {
		t.Fatal("unreadable source was renamed")
	}
}

func TestRenameImportedProxyConflicts(t *testing.T) {
	for _, kind := range []string{"content", "url", "same-source-cross-protocol"} {
		t.Run(kind, func(t *testing.T) {
			s, key := renameFixture(t)
			src, _, err := s.Save(context.Background(), "", Input{Name: "other", Content: "http://user:pass@other.example:8080#Taken", Interval: 60})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "url" {
				sealed, e := s.Vault.Seal([]byte("https://example.com/sub"))
				if e != nil {
					t.Fatal(e)
				}
				_, err = s.Database.Exec("UPDATE subscription_sources SET url_encrypted=? WHERE id=?", sealed, src)
			}
			if kind == "same-source-cross-protocol" {
				_, err = s.Database.Exec("UPDATE imported_proxies SET source_id=(SELECT source_id FROM imported_proxies WHERE id=?) WHERE source_id=?", key, src)
			}
			if err != nil {
				t.Fatal(err)
			}
			w := renameRequest(s, key, `{"name":"Taken","expectedRevision":1}`)
			wantKind := kind
			if kind == "same-source-cross-protocol" {
				wantKind = "content"
			}
			if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"import_name_conflict"`) || !strings.Contains(w.Body.String(), `"type":"`+wantKind+`"`) || !strings.Contains(w.Body.String(), `"name":"Taken"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRenameImportedProxyWriteFailureRollsBack(t *testing.T) {
	s, key := renameFixture(t)
	if _, err := s.Database.Exec(`CREATE TRIGGER reject_rename BEFORE UPDATE ON imported_proxies BEGIN SELECT RAISE(ABORT,'rename rejected'); END`); err != nil {
		t.Fatal(err)
	}
	w := renameRequest(s, key, `{"name":"new","expectedRevision":1}`)
	if w.Code != 500 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	p, name, revision, _ := renamePayload(t, s, key)
	if name != "Original" || p.Name != name || revision != 1 {
		t.Fatal("failed write changed state")
	}
}

func TestRenameImportedProxyPreservesCanvasReferences(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	src, _, err := s.Save(ctx, "", Input{Name: "chain", Interval: 60, Content: "socks5://user:pass@a.example:1080#A\nsocks5://user:pass@b.example:1080#B"})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, src, "")
	viewer := source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}}
	entries, _, err := s.Proxies(ctx, viewer)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	layout := canvas.CanvasLayout{Instances: []canvas.CanvasInstance{{InstanceID: "a", Ref: "import:" + entries[0].Origin.Ref, Role: "entry"}, {InstanceID: "b", Ref: "import:" + entries[1].Origin.Ref, Role: "exit"}}, Connections: []canvas.CanvasConnection{{ID: "edge", Source: "a", Target: "b", Type: "proxy_chain", SupportTCP: true}}}
	raw, err := json.Marshal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at,published_at) VALUES('rename-chain','chain','client',?,?,'now','now','now')", string(raw), string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Rename(ctx, entries[0].Origin.Ref, RenameInput{Name: "Renamed", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	var draft, published string
	if err = s.Database.QueryRow("SELECT layout_json,client_published_layout FROM canvas_schemes WHERE id='rename-chain'").Scan(&draft, &published); err != nil {
		t.Fatal(err)
	}
	if draft != string(raw) || published != string(raw) {
		t.Fatal("rename changed canvas references")
	}
	entries, _, err = s.Proxies(ctx, viewer)
	if err != nil {
		t.Fatal(err)
	}
	chained, warnings := s.Chains(ctx, entries)
	if len(chained) != 4 || len(warnings) != 0 || chained[3].DialerProxy != chained[2].Name {
		t.Fatalf("chain=%v warnings=%v", chained, warnings)
	}
}

func TestRenameImportedProxySelfBuiltNameConflict(t *testing.T) {
	s, key := renameFixture(t)
	s.SelfBuiltNames = published.EndpointNameStore{Vault: s.Vault, CanvasEntries: &canvas.Repository{Database: s.Reader}}
	if _, err := s.Database.Exec("INSERT INTO nodes(id,name,status,created_at) VALUES('rename-node','Edge','online','now')"); err != nil {
		t.Fatal(err)
	}
	for _, in := range []struct {
		id   string
		port int
	}{{"one", 443}, {"two", 8443}} {
		if _, err := s.Database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?,?,'rename-node','vless','0.0.0.0',?,1,1,0,'direct','[]',x'00','now')`, in.id, in.id, in.port); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Edge · vless", "Edge · vless · 443", "Edge · vless · 8443"} {
		body, err := json.Marshal(RenameInput{Name: name, ExpectedRevision: 1})
		if err != nil {
			t.Fatal(err)
		}
		w := renameRequest(s, key, string(body))
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"type":"self-built"`) || !strings.Contains(w.Body.String(), `"name":"`+name+`"`) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if _, err := s.Database.Exec(`INSERT INTO canvas_entry_bindings(id,instance_id,inbound_id,mode,state,updated_at) VALUES('binding','entry','one','create','detached','now')`); err != nil {
		t.Fatal(err)
	}
	w := renameRequest(s, key, `{"name":"one","expectedRevision":1}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"type":"self-built"`) || !strings.Contains(w.Body.String(), `"name":"one"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
