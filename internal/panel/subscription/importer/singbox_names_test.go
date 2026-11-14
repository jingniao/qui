package importer

import (
	"context"
	"encoding/json"
	"testing"

	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/source"

	"gopkg.in/yaml.v3"
)

func TestSingBoxRenameContentAndChainPreservesClashNames(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	src, _, err := s.Save(ctx, "", Input{Name: "manual", Interval: 60, Content: "socks5://user:pass@a.example:1080#A\nsocks5://user:pass@b.example:1080#B"})
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
	if _, err = s.Database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,client_published_layout,created_at,updated_at,published_at) VALUES('rename','chain','client','{}',?,'now','now','now')", string(raw)); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"新入口", "新出口"} {
		if _, err = s.Rename(ctx, entries[i].Origin.Ref, RenameInput{Name: name, ExpectedRevision: 1}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _, err = s.Proxies(ctx, viewer)
	if err != nil {
		t.Fatal(err)
	}
	entries, warnings := s.Chains(ctx, entries)
	if len(entries) != 4 || len(warnings) != 0 {
		t.Fatalf("chains=%v warnings=%v", entries, warnings)
	}
	for _, p := range entries {
		if p.Origin.ImportKind != "content" {
			t.Fatalf("missing content metadata: %+v", p.Origin)
		}
	}
	out, err := (singbox.Exporter{}).Render(format.Profile{Proxies: entries})
	if err != nil || len(out.Warnings) != 2 {
		t.Fatalf("sing-box: %v %v", err, out.Warnings)
	}
	var doc struct {
		Outbounds []struct{ Tag string }
	}
	if err := json.Unmarshal(out.Body, &doc); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range doc.Outbounds {
		seen[p.Tag] = true
	}
	for _, tag := range []string{"新入口", "新出口"} {
		if _, ok := seen[tag]; !ok {
			t.Errorf("missing tag %q: %s", tag, out.Body)
		}
	}
	if len(doc.Outbounds) != 2 {
		t.Errorf("client chain was not omitted: %s", out.Body)
	}
	clashOut, err := (clash.Meta{}).Render(format.Profile{Proxies: entries})
	if err != nil {
		t.Fatal(err)
	}
	var clashDoc struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Dialer string `yaml:"dialer-proxy"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(clashOut.Body, &clashDoc); err != nil {
		t.Fatal(err)
	}
	if len(clashDoc.Proxies) != 4 || clashDoc.Proxies[2].Name != "chain-rename-a" || clashDoc.Proxies[3].Name != "chain-rename-b" || clashDoc.Proxies[3].Dialer != "chain-rename-a" {
		t.Fatalf("Clash names changed: %s", clashOut.Body)
	}
}

func TestSingBoxURLImportKeepsLegacyTag(t *testing.T) {
	s, userID, _ := storeFixture(t)
	ctx := context.Background()
	// Seed a URL source's cached entry without fetching over the network.
	src, _, err := s.Save(ctx, "", Input{Name: "airport", Interval: 60, Content: "socks5://user:pass@a.example:1080#Airport"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Vault.Seal([]byte("https://example.com/sub"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("UPDATE subscription_sources SET url_encrypted=? WHERE id=?", sealed, src); err != nil {
		t.Fatal(err)
	}
	grantImport(t, s, userID, src, "")
	entries, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if entries[0].Origin.ImportKind != "url" {
		t.Fatalf("URL metadata=%+v", entries[0].Origin)
	}
	out, err := (singbox.Exporter{}).Render(format.Profile{Proxies: entries})
	if err != nil || len(out.Warnings) != 0 {
		t.Fatalf("sing-box: %v %v", err, out.Warnings)
	}
	var doc struct{ Outbounds []struct{ Tag string } }
	if err := json.Unmarshal(out.Body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Outbounds) != 1 || doc.Outbounds[0].Tag != "-"+entries[0].Origin.Ref+"-socks5" {
		t.Fatalf("URL tag changed: %s", out.Body)
	}
}
