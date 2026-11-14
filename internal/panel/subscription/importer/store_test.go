package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
)

func storeFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	d, err := dbtest.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	// The panel gives the store a separate read pool; the fixture does the same, so a
	// test can hold the only writer connection while reads keep working.
	reader, err := dbtest.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	v, err := vault.FromKey(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: d, Vault: v}
	ids := []string{}
	for _, name := range []string{"import_a", "import_b"} {
		u, e := users.Save(ctx, "", user.Input{Username: name, Status: "active", AllowedProtocols: []string{"socks5"}})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, u.ID)
	}
	return &Store{Database: d, Reader: reader, Vault: v}, ids[0], ids[1]
}
func grantImport(t *testing.T, s *Store, userID, sourceID, proxyKey string) string {
	t.Helper()
	ctx := context.Background()
	nodes := &node.Repository{Database: s.Database, Vault: s.Vault}
	imports := []node.GroupImport{{SourceID: sourceID, ProxyKey: proxyKey}}
	var sealedURL []byte
	if err := s.Database.QueryRow("SELECT url_encrypted FROM subscription_sources WHERE id=?", sourceID).Scan(&sealedURL); err != nil {
		t.Fatal(err)
	}
	kind, err := s.sourceKindStrict(sealedURL)
	if err != nil {
		t.Fatal(err)
	}
	if kind == sourceKindContent {
		// Existing fixtures select by name or all current content nodes; persist
		// that selection using explicit IDs under the new write contract.
		rows, err := s.Database.Query("SELECT id FROM imported_proxies WHERE source_id=? AND (?='' OR name||'|'||protocol=?) ORDER BY id", sourceID, proxyKey, proxyKey)
		if err != nil {
			t.Fatal(err)
		}
		imports = nil
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			imports = append(imports, node.GroupImport{SourceID: sourceID, ProxyKey: "id:" + id})
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(imports) == 0 {
			t.Fatal("content fixture selected no nodes")
		}
	}
	g, e := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "import " + sourceID + proxyKey, Purpose: "subscription", Enabled: true, SubscriptionVisible: true, Imports: imports})
	if e != nil {
		t.Fatal(e)
	}
	if e = (&user.Repository{Database: s.Database, Vault: s.Vault}).Grant(ctx, userID, g.ID, false); e != nil {
		t.Fatal(e)
	}
	return g.ID
}
func TestEncryptedImportsGrantsRulesAndFailureWarning(t *testing.T) {
	s, a, b := storeFixture(t)
	ctx := context.Background()
	const content = "socks5://fixture:private-fixture@example.com:1080#external"
	key, _, err := s.Save(ctx, "", Input{Name: "test import", Content: content, Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, a, key, "")
	var sealed []byte
	if err = s.Database.QueryRow("SELECT payload_encrypted FROM imported_proxies").Scan(&sealed); err != nil || bytes.Contains(sealed, []byte("private-fixture")) {
		t.Fatal("payload not encrypted")
	}
	if p, _, e := s.Proxies(ctx, source.Viewer{UserID: b, AllowedProtocols: []string{"socks5"}}); e != nil || len(p) != 0 {
		t.Fatal("grant isolation")
	}
	viewer := source.Viewer{UserID: a, AllowedProtocols: []string{"socks5"}}
	p, _, err := s.Proxies(ctx, viewer)
	if err != nil || len(p) != 1 || p[0].Origin.Source != "import:"+key {
		t.Fatal("granted import missing")
	}
	template := rules.Default()
	template.Groups[0].Members = []string{"@source:" + key}
	resolved := rules.Expand(template, p)
	if len(resolved.Groups[0].Members) != 1 || resolved.Groups[0].Members[0].Kind != rules.KindProxy {
		t.Fatal("source expansion")
	}
	resolved = rules.Expand(template, nil)
	if resolved.Groups[0].Members[0].Kind == rules.KindProxy {
		t.Fatal("ungranted source expanded")
	}
	// Failed sources, including an empty first fetch, contribute only warnings.
	if _, err = s.Database.Exec("UPDATE subscription_sources SET last_status='failed'"); err != nil {
		t.Fatal(err)
	}
	p, w, err := source.NewRegistry(s).Collect(ctx, viewer)
	if err != nil || len(p) != 0 || len(w) == 0 {
		t.Fatal("failed source broke subscription")
	}
	if _, err = s.Database.Exec("DELETE FROM imported_proxies"); err != nil {
		t.Fatal(err)
	}
	_, w, err = s.Proxies(ctx, viewer)
	if err != nil || len(w) == 0 {
		t.Fatal("failed first fetch lost warning")
	}
	// URL encryption and fetch failures belong to a separate URL source.
	urlKey, _, err := s.Save(ctx, "", Input{Name: "test URL import", URL: "https://example.com/fixture", Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, a, urlKey, "")
	if err = s.Database.QueryRow("SELECT url_encrypted FROM subscription_sources WHERE id=?", urlKey).Scan(&sealed); err != nil || bytes.Contains(sealed, []byte("example.com")) {
		t.Fatal("URL not encrypted")
	}
	s.Fetcher.Resolver = &fakeResolver{answers: [][]netip.Addr{addresses("127.0.0.1")}}
	if _, err = s.Refresh(ctx, urlKey); !errors.Is(err, ErrFetch) {
		t.Fatalf("refresh error = %v, want ErrFetch", err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 2 || items[1].ID != urlKey || items[1].Status != "failed" {
		t.Fatal("refresh failure not recorded")
	}
	p, w, err = source.NewRegistry(s).Collect(ctx, viewer)
	if err != nil || len(p) != 0 || len(w) != 2 {
		t.Fatalf("failed sources lost warnings: %+v %+v %v", p, w, err)
	}
	// Seed a legacy grant only to verify deletion still cascades it.
	if _, err = s.Database.Exec("INSERT INTO user_source_grants(user_id,source_id) VALUES(?,?)", a, urlKey); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, urlKey); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.Database.QueryRow("SELECT count(*) FROM user_source_grants").Scan(&count); err != nil || count != 0 {
		t.Fatal("delete cascade")
	}
	if err = s.Database.QueryRow("SELECT count(*) FROM node_group_imports").Scan(&count); err != nil || count != 0 {
		t.Fatal("group import delete cascade")
	}
}

// The node list tells 机场订阅 from 单独导入 by this field only, so the derived
// kind must survive an edit that keeps the stored payload, and must appear in the
// JSON the panel serves (the struct that also carries lastFetchAt).
func TestSourceKindDistinguishesSubscriptionFromContent(t *testing.T) {
	s, a, _ := storeFixture(t)
	ctx := context.Background()
	urlKey, _, err := s.Save(ctx, "", Input{Name: "airport", URL: "https://example.com/fixture", Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	contentKey, _, err := s.Save(ctx, "", Input{Name: "manual", Content: "socks5://fixture:fixture@example.com:1080#manual", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	kinds := func() map[string]string {
		t.Helper()
		items, e := s.List(ctx)
		if e != nil {
			t.Fatal(e)
		}
		found := map[string]string{}
		for _, item := range items {
			found[item.ID] = item.Kind
		}
		return found
	}
	if got := kinds(); got[urlKey] != sourceKindURL || got[contentKey] != sourceKindContent {
		t.Fatalf("kind not derived from the stored payload: %v", got)
	}
	// Renaming keeps url_encrypted untouched, so neither kind may flip.
	if _, _, err = s.Save(ctx, contentKey, Input{Name: "manual renamed", Interval: 120}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Save(ctx, urlKey, Input{Name: "airport renamed", URL: "https://example.com/other", Interval: 120}); err != nil {
		t.Fatal(err)
	}
	if got := kinds(); got[urlKey] != sourceKindURL || got[contentKey] != sourceKindContent {
		t.Fatalf("edit changed the derived kind: %v", got)
	}
	items, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(items)
	if err != nil || !bytes.Contains(raw, []byte(`"kind":"content"`)) || !bytes.Contains(raw, []byte(`"kind":"url"`)) || !bytes.Contains(raw, []byte(`"lastFetchAt"`)) {
		t.Fatalf("kind missing from the source view: %s", raw)
	}
}

func TestClientChainUsesPublishedSnapshotAndAllGrants(t *testing.T) {
	s, a, b := storeFixture(t)
	ctx := context.Background()
	key, _, err := s.Save(ctx, "", Input{Name: "chain inputs", Content: "socks5://fixture:fixture@a.example.com:1080#A\nsocks5://fixture:fixture@b.example.com:1080#B", Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, a, key, "")
	viewer := source.Viewer{UserID: a, AllowedProtocols: []string{"socks5"}}
	p, _, err := s.Proxies(ctx, viewer)
	if err != nil || len(p) != 2 {
		t.Fatal("inputs")
	}
	layout := canvas.CanvasLayout{Instances: []canvas.CanvasInstance{{InstanceID: "a", Ref: "import:" + p[0].Origin.Ref, Role: "entry"}, {InstanceID: "b", Ref: "import:" + p[1].Origin.Ref, Role: "exit"}}, Connections: []canvas.CanvasConnection{{ID: "edge", Source: "a", Target: "b", Type: "proxy_chain", SupportTCP: true}}}
	raw, _ := json.Marshal(layout)
	if _, err = s.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at,published_at) VALUES('test-chain','chain','client','{}',?,?,?,?)", string(raw), "now", "now", "now"); err != nil {
		t.Fatal(err)
	}
	chained, w := s.Chains(ctx, p)
	if len(chained) != 4 || len(w) != 0 || chained[3].DialerProxy != chained[2].Name {
		t.Fatal("published snapshot not rendered")
	}
	// The merged UI emits tunnel; it retains the exact client dialer-proxy semantics.
	layout.Connections[0].Type = "tunnel"
	raw, _ = json.Marshal(layout)
	if _, err = s.Database.Exec("UPDATE canvas_schemes SET client_published_layout=? WHERE id='test-chain'", string(raw)); err != nil {
		t.Fatal(err)
	}
	tunnel, tunnelWarnings := s.Chains(ctx, p)
	if len(tunnelWarnings) != 0 || len(tunnel) != len(chained) || tunnel[3].DialerProxy != chained[3].DialerProxy || tunnel[2].Name != chained[2].Name {
		t.Fatal("tunnel alias changed client chain", tunnel, tunnelWarnings)
	}
	other, _, err := s.Proxies(ctx, source.Viewer{UserID: b, AllowedProtocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	chained, w = s.Chains(ctx, other)
	if len(chained) != 0 || len(w) == 0 {
		t.Fatal("chain leaked across grants")
	}
	// Reimporting a reordered batch creates new identities; it cannot retarget
	// the published snapshot, even once the new source is also granted.
	before := identitySnapshot(t, s, key, true)
	newKey, _, err := s.Save(ctx, "", Input{Name: "reordered inputs", Content: "socks5://fixture:fixture@b.example.com:1080#B\nsocks5://fixture:fixture@a.example.com:1080#A", Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	if after := identitySnapshot(t, s, key, true); after != before {
		t.Fatal("new batch changed the published source")
	}
	unchanged, _, err := s.Proxies(ctx, viewer)
	if err != nil || !reflect.DeepEqual(unchanged, p) {
		t.Fatalf("new batch inherited existing grants: %+v %v", unchanged, err)
	}
	newGroup := grantImport(t, s, a, newKey, "")
	reimported, _, err := s.Proxies(ctx, viewer)
	if err != nil || len(reimported) != 4 {
		t.Fatalf("reimported inputs: %+v %v", reimported, err)
	}
	chained, w = s.Chains(ctx, reimported)
	if len(chained) != 6 || len(w) != 0 || !reflect.DeepEqual(chained[4:], tunnel[2:]) {
		t.Fatal("new batch retargeted the published chain", chained, w)
	}
	// A viewer granted only the new batch cannot use a chain of the old IDs.
	if err = (&user.Repository{Database: s.Database, Vault: s.Vault}).Grant(ctx, b, newGroup, false); err != nil {
		t.Fatal(err)
	}
	other, _, err = s.Proxies(ctx, source.Viewer{UserID: b, AllowedProtocols: []string{"socks5"}})
	if err != nil || len(other) != 2 {
		t.Fatalf("new batch grant: %+v %v", other, err)
	}
	chained, w = s.Chains(ctx, other)
	if len(chained) != 2 || len(w) == 0 {
		t.Fatal("same-name new nodes satisfied old chain grants", chained, w)
	}
}

func TestImportSchedulerStopsAndCancelsFetch(t *testing.T) {
	s, a, _ := storeFixture(t)
	key, _, err := s.Save(context.Background(), "", Input{Name: "scheduled", URL: "https://example.com/fixture", Interval: 60, UserIDs: []string{a}})
	if err != nil || key == "" {
		t.Fatal(err)
	}
	started := make(chan struct{})
	s.Fetcher.Resolver = &cancelResolver{started: started}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
	items, err := s.List(context.Background())
	if err != nil || items[0].Status != "pending" {
		t.Fatal("cancelled fetch wrote a failure")
	}
}

type cancelResolver struct{ started chan struct{} }

func (r *cancelResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	close(r.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
