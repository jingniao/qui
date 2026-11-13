package policy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

const (
	nativeSingBoxURL = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"
	nativeXrayURL    = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"
)

func nativeInput() SourceInput {
	return SourceInput{URL: nativeSingBoxURL, Format: "native", Behavior: "domain", IntervalSeconds: 86400, XrayURL: nativeXrayURL, XrayTag: "cn"}
}

func TestSaveNativeSourceValidation(t *testing.T) {
	service, _, _, _, _ := fixture(t)
	ctx := context.Background()
	svc, err := service.Repository.SaveService(ctx, "", ServiceInput{Name: "native validation", Enabled: true, Domains: []string{"a.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(edit func(*SourceInput)) SourceInput {
		in := nativeInput()
		edit(&in)
		return in
	}
	bad := map[string]SourceInput{
		"foreign host": mutate(func(in *SourceInput) {
			in.URL = "https://example.com/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"
		}),
		"foreign xray":         mutate(func(in *SourceInput) { in.XrayURL = "https://example.com/geosite.dat" }),
		"http":                 mutate(func(in *SourceInput) { in.URL = strings.Replace(nativeSingBoxURL, "https", "http", 1) }),
		"behavior vs file":     mutate(func(in *SourceInput) { in.Behavior = "ipcidr" }),
		"classical":            mutate(func(in *SourceInput) { in.Behavior = "classical" }),
		"missing tag":          mutate(func(in *SourceInput) { in.XrayTag = "" }),
		"missing xray url":     mutate(func(in *SourceInput) { in.XrayURL = "" }),
		"bad tag":              mutate(func(in *SourceInput) { in.XrayTag = "CN CN" }),
		"non-native with xray": {URL: "https://example.com/rules.yaml", Format: "yaml", Behavior: "domain", XrayURL: nativeXrayURL, XrayTag: "cn"},
		"non-native tag only":  {URL: "https://example.com/rules.yaml", Format: "yaml", Behavior: "domain", XrayTag: "cn"},
	}
	for name, in := range bad {
		if err := service.Repository.SaveSources(ctx, nil, svc.ID, []SourceInput{in}); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	if err := service.Repository.SaveSources(ctx, nil, svc.ID, []SourceInput{nativeInput()}); err != nil {
		t.Fatal(err)
	}
	saved, err := service.Repository.Service(ctx, svc.ID)
	if err != nil || len(saved.Sources) != 1 {
		t.Fatal(saved, err)
	}
	source := saved.Sources[0]
	if source.Format != "native" || source.XrayURL != nativeXrayURL || source.XrayTag != "cn" || !source.NodeUsable || source.ClientUsable {
		t.Fatalf("%+v", source)
	}
	// Saving the set again with the same ids keeps the source and its usability.
	ip := nativeInput()
	ip.ID = source.ID
	if err := service.Repository.SaveSources(ctx, nil, svc.ID, []SourceInput{ip}); err != nil {
		t.Fatal(err)
	}
}

func TestForNodeExpandsNativeSourcesOnlyForCapableNodes(t *testing.T) {
	service, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	svc, err := service.Repository.SaveService(ctx, "", ServiceInput{Name: "native cn", Enabled: true, Domains: []string{"hand.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Repository.SaveSources(ctx, nil, svc.ID, []SourceInput{
		{URL: "https://example.com/rules.txt", Format: "text", Behavior: "domain", IntervalSeconds: 86400},
		nativeInput(),
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := service.Repository.Service(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var yamlID, nativeID string
	for _, src := range saved.Sources {
		if src.Format == "native" {
			nativeID = src.ID
		} else {
			yamlID = src.ID
		}
	}
	raw, _ := json.Marshal(ParsedEntries{Domains: []string{"cached.example.com"}})
	sealed, err := service.Repository.Vault.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := service.Repository.Database.ExecContext(ctx, `UPDATE service_set_sources SET last_status='ok', last_success_at=?, last_fetch_at=?, entry_count=1, encrypted_cache=? WHERE id=?`, now, now, sealed, yamlID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, "", Input{Name: "block cn", Priority: 5, Enabled: true, Match: desiredstate.Match{ServiceIDs: []string{svc.ID}}, Action: desiredstate.Action{Type: "Block"}}); err != nil {
		t.Fatal(err)
	}
	find := func() desiredstate.Policy {
		policies, err := service.ForNode(ctx, nodeID)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range policies {
			if p.Name == "block cn" {
				return p
			}
		}
		t.Fatal("policy missing")
		return desiredstate.Policy{}
	}
	caps := func(cores ...desiredstate.CoreKeywordCapability) {
		service.NodeCapabilities = func(context.Context, string) ([]desiredstate.CoreKeywordCapability, error) { return cores, nil }
	}

	// Old node: the native source contributes nothing, the cached source expands.
	caps(desiredstate.CoreKeywordCapability{Name: "xray"})
	old := find()
	if len(old.Match.RuleSets) != 0 || !contains(old.Match.Domains, "cached.example.com") || !contains(old.Match.Domains, "hand.example.com") {
		t.Fatalf("old node: %+v", old.Match)
	}
	// No capability callback: same as an old node.
	service.NodeCapabilities = nil
	if got := find(); len(got.Match.RuleSets) != 0 || !contains(got.Match.Domains, "cached.example.com") {
		t.Fatalf("no capabilities: %+v", got.Match)
	}
	// A native-capable xray core plus a GOST core without the bit.
	caps(desiredstate.CoreKeywordCapability{Name: "xray", NativeRuleSetsSupported: true}, desiredstate.CoreKeywordCapability{Name: desiredstate.CoreGost})
	current := find()
	if len(current.Match.RuleSets) != 1 {
		t.Fatalf("capable node: %+v", current.Match)
	}
	want := desiredstate.RuleSet{ID: nativeID, Behavior: "domain", SingBoxURL: nativeSingBoxURL, XrayURL: nativeXrayURL, XrayTag: "cn", RefreshSeconds: 86400}
	if current.Match.RuleSets[0] != want {
		t.Fatalf("%+v", current.Match.RuleSets[0])
	}
	if contains(current.Match.Domains, "cached.example.com") || !contains(current.Match.Domains, "hand.example.com") {
		t.Fatalf("yaml source must be skipped, hand entries kept: %v", current.Match.Domains)
	}
	if !current.Enabled {
		t.Fatal("policy disabled")
	}
	// One core without the bit makes the whole node non-native.
	caps(desiredstate.CoreKeywordCapability{Name: "xray", NativeRuleSetsSupported: true}, desiredstate.CoreKeywordCapability{Name: "sing-box"})
	if got := find(); len(got.Match.RuleSets) != 0 {
		t.Fatalf("mixed node: %+v", got.Match)
	}

	// A set with only a native source and no entries still enables the policy.
	only, err := service.Repository.SaveService(ctx, "", ServiceInput{Name: "native only", Enabled: true, Sources: &[]SourceInput{nativeInput()}})
	if err != nil {
		t.Fatal(err)
	}
	caps(desiredstate.CoreKeywordCapability{Name: "xray", NativeRuleSetsSupported: true})
	resolved, err := service.resolve(ctx, desiredstate.Policy{ID: "o", Name: "o", Priority: 5, Enabled: true, Match: desiredstate.Match{ServiceIDs: []string{only.ID}}, Action: desiredstate.Action{Type: "Block"}}, true)
	if err != nil || !resolved.Enabled || len(resolved.Match.RuleSets) != 1 {
		t.Fatal(resolved, err)
	}
	resolved, err = service.resolve(ctx, desiredstate.Policy{ID: "o", Name: "o", Priority: 5, Enabled: true, Match: desiredstate.Match{ServiceIDs: []string{only.ID}}, Action: desiredstate.Action{Type: "Block"}}, false)
	if err != nil || resolved.Enabled {
		t.Fatalf("native-only set on an old node must disable the policy: %+v %v", resolved, err)
	}
}

func TestNativeSourceIsNeverFetched(t *testing.T) {
	service, _, _, _, _ := fixture(t)
	ctx := context.Background()
	svc, err := service.Repository.SaveService(ctx, "", ServiceInput{Name: "native refresh", Enabled: true, Domains: []string{"a.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Repository.SaveSources(ctx, nil, svc.ID, []SourceInput{nativeInput()}); err != nil {
		t.Fatal(err)
	}
	saved, _ := service.Repository.Service(ctx, svc.ID)
	fetcher := &countingFetcher{}
	refresher := &SourceRefresher{Repository: service.Repository, Fetcher: fetcher, Vault: service.Repository.Vault}
	if _, err := refresher.Refresh(ctx, saved.Sources[0].ID); !errors.Is(err, ErrNativeSourceRefresh) {
		t.Fatalf("refresh error = %v", err)
	}
	if fetcher.calls != 0 {
		t.Fatal("native source was fetched")
	}
	for _, id := range refresher.dueSources(ctx, []string{svc.ID}) {
		if id == saved.Sources[0].ID {
			t.Fatal("native source is due")
		}
	}
	for _, id := range refresher.dueSources(ctx, nil) {
		if id == saved.Sources[0].ID {
			t.Fatal("native source is due in the periodic check")
		}
	}
	entries, err := service.Repository.SourceEntries(ctx, svc.ID)
	if err != nil || len(entries.Domains)+len(entries.Suffixes)+len(entries.CIDRs) != 0 {
		t.Fatal(entries, err)
	}
}

type countingFetcher struct{ calls int }

func (f *countingFetcher) Fetch(context.Context, string) ([]byte, error) {
	f.calls++
	return nil, errors.New("must not fetch")
}
