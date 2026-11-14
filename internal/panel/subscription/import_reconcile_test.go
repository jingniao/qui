package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/node"
	panelproxy "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/importer"
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
)

func reconcileTestSource(t *testing.T, p *rulesPanel, sourceID, url, status string, entries ...subproxy.Proxy) {
	t.Helper()
	sealed, err := p.users.Vault.Seal([]byte(url))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,last_status,created_at,updated_at) VALUES(?,?,?,60,?,'old','old')", sourceID, sourceID, sealed, status); err != nil {
		t.Fatal(err)
	}
	for index, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err = p.users.Vault.Seal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.database.Exec("INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES(?,?,?,?,?,'old')", sourceID+"-"+string(rune('a'+index)), sourceID, entry.Name, entry.Protocol, sealed); err != nil {
			t.Fatal(err)
		}
	}
}

func reconcileTestRun(t *testing.T, ctx context.Context, p *rulesPanel, imports *importer.Store) error {
	return imports.ReconcileImportedIdentities(ctx, func(ctx context.Context, tx *sql.Tx, resolve func(string, string) ([]string, bool), rows map[string]string) ([]string, error) {
		var report []string
		for _, rewrite := range []func(context.Context, *sql.Tx, func(string, string) ([]string, bool)) ([]string, error){node.ReconcileImportIdentitiesTx, rules.ReconcileImportIdentitiesTx} {
			items, err := rewrite(ctx, tx, resolve)
			if err != nil {
				return nil, err
			}
			report = append(report, items...)
		}
		items, err := panelproxy.ReconcileImportRefsTx(ctx, tx, func(old string) (string, bool) {
			if next, ok := rows[old]; ok {
				return next, true
			}
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM imported_proxies WHERE id=?", old).Scan(&count); err != nil {
				t.Fatal(err)
			}
			return old, count != 0
		})
		return append(report, items...), err
	})
}

func TestImportReconcilePreservesUserSubscriptionAuthorization(t *testing.T) {
	p := newRulesPanel(t)
	ctx := context.Background()
	imports := &importer.Store{Database: p.database, Vault: p.users.Vault}
	p.service.Sources.Register(imports)
	endpoint := func(name, host string, port int) subproxy.Proxy {
		return subproxy.Proxy{Name: name, Server: host, Port: port, Protocol: "socks5", Credential: subproxy.Credential{Username: "user", Password: "secret"}}
	}
	reconcileTestSource(t, p, "content", "", "ok", endpoint("Shared", "one.example", 1080), endpoint("Shared", "two.example", 1081), endpoint("Other", "other.example", 1082))
	reconcileTestSource(t, p, "failed", "", "failed", endpoint("Failed", "failed.example", 1080))
	reconcileTestSource(t, p, "airport", "https://airport.example/sub", "ok", endpoint("Airport", "airport.example", 1080))
	reconcileTestSource(t, p, "empty", "", "ok")
	for _, member := range []struct{ group, src, key string }{
		{p.groups[0].ID, "content", ""}, {p.groups[0].ID, "content", "Shared|socks5"},
		{p.groups[1].ID, "content", "Shared|socks5"}, {p.groups[1].ID, "content", "Missing|socks5"},
		{p.groups[1].ID, "airport", ""}, {p.groups[1].ID, "airport", "Airport|socks5"},
		{p.groups[1].ID, "failed", ""}, {p.groups[1].ID, "empty", ""},
	} {
		if _, err := p.database.Exec("INSERT INTO node_group_imports VALUES(?,?,?)", member.group, member.src, member.key); err != nil {
			t.Fatal(err)
		}
	}
	other, err := p.users.Save(ctx, "", user.Input{Username: "only_exact", Status: "active", NodeGroupIDs: []string{p.groups[1].ID}})
	if err != nil {
		t.Fatal(err)
	}
	none, err := p.users.Save(ctx, "", user.Input{Username: "no_imports", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, userID := range []string{p.u.ID, other.ID, none.ID} {
		_, token, err := p.service.Rotate(ctx, userID, "")
		if err != nil {
			t.Fatal(err)
		}
		tokens[userID] = token
	}
	type snapshot struct {
		endpoints []string
		labels    map[string][]string
		output    []string
	}
	read := func() map[string]snapshot {
		all := map[string]snapshot{}
		for userID, token := range tokens {
			entries, _, err := imports.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}})
			if err != nil {
				t.Fatal(err)
			}
			state := snapshot{labels: map[string][]string{}}
			for _, entry := range entries {
				key := entry.Server + ":" + entry.Name
				state.endpoints = append(state.endpoints, key)
				sort.Strings(entry.Origin.Labels)
				state.labels[key] = entry.Origin.Labels
			}
			sort.Strings(state.endpoints)
			response := p.do(t, "GET", "/sub/"+token+"?format=clash", "", nil, "")
			var document struct {
				Proxies []struct {
					Name, Server string
					Port         int
				}
			}
			if response.Code != 200 || yaml.Unmarshal(response.Body.Bytes(), &document) != nil {
				t.Fatalf("subscription: %d %s", response.Code, response.Body.String())
			}
			for _, entry := range document.Proxies {
				state.output = append(state.output, entry.Server+":"+entry.Name)
			}
			sort.Strings(state.output)
			all[userID] = state
		}
		return all
	}
	before := read()
	var urlBefore string
	if err := p.database.QueryRow("SELECT id||':'||hex(payload_encrypted)||':'||fetched_at||':'||revision FROM imported_proxies WHERE source_id='airport'").Scan(&urlBefore); err != nil {
		t.Fatal(err)
	}
	if len(before[p.u.ID].endpoints) != 4 || len(before[other.ID].endpoints) != 3 || len(before[none.ID].endpoints) != 0 {
		t.Fatalf("unexpected pre-conversion authorization: %+v", before)
	}
	if err := reconcileTestRun(t, ctx, p, imports); err != nil {
		t.Fatal(err)
	}
	if after := read(); !reflect.DeepEqual(before, after) {
		t.Fatalf("authorization changed: before=%+v after=%+v", before, after)
	}
	var urlAfter string
	if err := p.database.QueryRow("SELECT id||':'||hex(payload_encrypted)||':'||fetched_at||':'||revision FROM imported_proxies WHERE source_id='airport'").Scan(&urlAfter); err != nil || urlAfter != urlBefore {
		t.Fatalf("URL cache changed: error=%v", err)
	}
	var count int
	if err := p.database.QueryRow("SELECT count(*) FROM import_identity_map WHERE source_id='failed'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed cache mappings=%d error=%v", count, err)
	}
	if err := p.database.QueryRow("SELECT count(*) FROM node_group_imports WHERE source_id='empty'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("empty source members=%d error=%v", count, err)
	}
	if err := p.database.QueryRow("SELECT count(*) FROM node_group_imports WHERE source_id='airport' AND proxy_key IN ('','Airport|socks5')").Scan(&count); err != nil || count != 2 {
		t.Fatalf("URL members=%d error=%v", count, err)
	}
	var report string
	if err := p.database.QueryRow("SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&report); err != nil || !strings.Contains(report, "Missing|socks5") {
		t.Fatalf("report=%s error=%v", report, err)
	}
}

func TestImportReconcileCanvasDraftAndPublishedReferences(t *testing.T) {
	p := newRulesPanel(t)
	ctx := context.Background()
	imports := &importer.Store{Database: p.database, Vault: p.users.Vault}
	entry := subproxy.Proxy{Name: "Node", Server: "node.example", Port: 1080, Protocol: "socks5"}
	reconcileTestSource(t, p, "content", "", "ok", entry)
	reconcileTestSource(t, p, "airport", "https://airport.example/sub", "ok", entry)
	layout := panelproxy.CanvasLayout{Instances: []panelproxy.CanvasInstance{{InstanceID: "first", Ref: "import:content-a", Role: "entry", X: 10, Y: 20}, {InstanceID: "second", Ref: "import:airport-a", Role: "exit", X: 30, Y: 40}, {InstanceID: "missing", Ref: "import:missing-row", Role: "relay"}}}
	raw, err := json.Marshal(layout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at,published_at) VALUES('scheme','scheme','client',?,?,'old','old','published')", string(raw), string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := reconcileTestRun(t, ctx, p, imports); err != nil {
		t.Fatal(err)
	}
	var newID string
	if err := p.database.QueryRow("SELECT new_proxy_id FROM import_identity_map WHERE old_proxy_id='content-a'").Scan(&newID); err != nil {
		t.Fatal(err)
	}
	layout.Instances[0].Ref = "import:" + newID
	var draft, published, updated, publishedAt string
	if err := p.database.QueryRow("SELECT layout_json,client_published_layout,updated_at,published_at FROM canvas_schemes WHERE id='scheme'").Scan(&draft, &published, &updated, &publishedAt); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{draft, published} {
		var got panelproxy.CanvasLayout
		if err := json.Unmarshal([]byte(data), &got); err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(layout)
		actual, _ := json.Marshal(got)
		if string(want) != string(actual) {
			t.Fatalf("layout=%s want=%s", actual, want)
		}
	}
	if updated == "old" || publishedAt != "published" {
		t.Fatalf("timestamps updated=%q published=%q", updated, publishedAt)
	}
	var report string
	if err := p.database.QueryRow("SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&report); err != nil || !strings.Contains(report, "missing-row") {
		t.Fatalf("report=%s error=%v", report, err)
	}
	if strings.Contains(report, "airport-a") {
		t.Fatalf("URL node incorrectly reported unresolved: %s", report)
	}
	if err := reconcileTestRun(t, ctx, p, imports); err != nil {
		t.Fatal(err)
	}
	var secondID string
	if err := p.database.QueryRow("SELECT id FROM imported_proxies WHERE source_id='content'").Scan(&secondID); err != nil || secondID != newID {
		t.Fatalf("repeated startup changed ID=%s error=%v", secondID, err)
	}
}
