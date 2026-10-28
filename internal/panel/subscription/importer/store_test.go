package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
)

func storeFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
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
	return &Store{Database: d, Vault: v}, ids[0], ids[1]
}
func TestEncryptedImportsGrantsRulesAndFailureWarning(t *testing.T) {
	s, a, b := storeFixture(t)
	ctx := context.Background()
	const content = "socks5://fixture:private-fixture@example.com:1080#external"
	key, err := s.Save(ctx, "", Input{Name: "test import", Content: content, Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err = s.Save(ctx, key, Input{Name: "test import", URL: "https://example.com/fixture", Interval: 60, UserIDs: []string{a}}); err != nil {
		t.Fatal(err)
	}
	if err = s.Database.QueryRow("SELECT url_encrypted FROM subscription_sources").Scan(&sealed); err != nil || bytes.Contains(sealed, []byte("example.com")) {
		t.Fatal("URL not encrypted")
	}
	s.Fetcher.Resolver = &fakeResolver{answers: [][]netip.Addr{addresses("127.0.0.1")}}
	if err = s.Refresh(ctx, key); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx)
	if err != nil || items[0].Status != "failed" {
		t.Fatal("refresh failure not recorded")
	}
	if err = s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.Database.QueryRow("SELECT count(*) FROM user_source_grants").Scan(&count); err != nil || count != 0 {
		t.Fatal("delete cascade")
	}
}

func TestClientChainUsesPublishedSnapshotAndAllGrants(t *testing.T) {
	s, a, b := storeFixture(t)
	ctx := context.Background()
	key, err := s.Save(ctx, "", Input{Name: "chain inputs", Content: "socks5://fixture:fixture@a.example.com:1080#A\nsocks5://fixture:fixture@b.example.com:1080#B", Interval: 60, UserIDs: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
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
	other, _, err := s.Proxies(ctx, source.Viewer{UserID: b, AllowedProtocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	chained, w = s.Chains(ctx, other)
	if len(chained) != 0 || len(w) == 0 {
		t.Fatal("chain leaked across grants")
	}
	// Reorder does not retarget opaque canvas references.
	if _, err = s.Save(ctx, key, Input{Name: "chain inputs", Content: "socks5://fixture:fixture@b.example.com:1080#B\nsocks5://fixture:fixture@a.example.com:1080#A", Interval: 60, UserIDs: []string{a}}); err != nil {
		t.Fatal(err)
	}
	reordered, _, _ := s.Proxies(ctx, viewer)
	if len(reordered) != 2 || reordered[0].Origin.Ref != p[0].Origin.Ref {
		t.Fatal("refresh changed stable identity")
	}
}

func TestImportSchedulerStopsAndCancelsFetch(t *testing.T) {
	s, a, _ := storeFixture(t)
	key, err := s.Save(context.Background(), "", Input{Name: "scheduled", URL: "https://example.com/fixture", Interval: 60, UserIDs: []string{a}})
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
