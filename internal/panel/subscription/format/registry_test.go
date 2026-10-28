package format

import (
	"errors"
	"testing"
)

// stubExporter is a format that exists only in these tests.
type stubExporter struct {
	id       string
	aliases  []string
	patterns []string
}

func (s stubExporter) ID() string          { return s.id }
func (s stubExporter) Aliases() []string   { return s.aliases }
func (s stubExporter) ContentType() string { return "text/plain" }
func (s stubExporter) FileName() string    { return s.id + ".txt" }
func (s stubExporter) UserAgents() []string {
	return s.patterns
}
func (s stubExporter) UsesRules() bool { return false }
func (s stubExporter) Render(Profile) (Output, error) {
	return Output{Body: []byte(s.id)}, nil
}

func TestRegistryResolvesIDsAndAliases(t *testing.T) {
	registry := NewRegistry(stubExporter{id: "uri"}, stubExporter{id: "mihomo", aliases: []string{"clash"}})
	for _, want := range []struct {
		query string
		id    string
	}{
		{"uri", "uri"},
		{"mihomo", "mihomo"},
		{"clash", "mihomo"},
	} {
		exporter, known := registry.Get(want.query)
		if !known || exporter.ID() != want.id {
			t.Fatalf("%q resolved to %v (%v)", want.query, exporter, known)
		}
	}
	if _, known := registry.Get("nope"); known {
		t.Fatal("an unknown ID resolved")
	}
	if registry.Known("") {
		t.Fatal("the empty ID resolved")
	}
	// Registration order is the list order the admin API reports.
	info := registry.Info()
	if len(info) != 2 || info[0].ID != "uri" || info[1].ID != "mihomo" || info[1].FileName != "mihomo.txt" {
		t.Fatalf("registry list: %+v", info)
	}
}

func TestRegistryRefusesDuplicateIDsAndAliases(t *testing.T) {
	registry := NewRegistry(stubExporter{id: "uri"}, stubExporter{id: "uri"}, stubExporter{id: "clash", aliases: []string{"uri"}}, stubExporter{id: "empty", aliases: []string{""}})
	ids := []string{}
	for _, exporter := range registry.Exporters() {
		ids = append(ids, exporter.ID())
	}
	// The duplicate ID and the aliased conflict are refused; an empty alias is not a
	// conflict.
	if len(ids) != 2 || ids[0] != "uri" || ids[1] != "empty" {
		t.Fatalf("registered %v", ids)
	}
}

func TestRegistryFallsBackWhenTheDefaultIsUnknown(t *testing.T) {
	registry := NewRegistry(stubExporter{id: "uri"}, stubExporter{id: "sing-box"})
	exporter, warnings, err := registry.Resolve("", "", Policy{Default: "retired"})
	if err != nil {
		t.Fatal(err)
	}
	if exporter.ID() != "uri" {
		t.Fatalf("fallback exporter %s", exporter.ID())
	}
	if len(warnings) != 1 || warnings[0].Ref != "uri" || warnings[0].Stage != "format" {
		t.Fatalf("fallback warnings: %+v", warnings)
	}
	if _, _, err := NewRegistry().Resolve("", "", Policy{}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("an empty registry must report an unknown format: %v", err)
	}
}

// The most specific pattern wins, a guard disqualifies its exporter and an
// unknown User-Agent resolves to nothing, so the default format serves it. No
// pattern ships in v1.1; these are test-only strings.
func TestRegistryMatchesTheMostSpecificUserAgent(t *testing.T) {
	registry := NewRegistry(
		stubExporter{id: "clash", patterns: []string{"clash"}},
		stubExporter{id: "clash-legacy", patterns: []string{"clashforwindows", "clashx/", "!meta"}},
		stubExporter{id: "sing-box", patterns: []string{"sing-box"}},
	)
	for _, want := range []struct {
		agent string
		id    string
	}{
		{"ClashForWindows/0.20.39", "clash-legacy"},
		{"clash-verge/v2.0.0", "clash"},
		{"ClashX/1.11.0", "clash-legacy"},
		{"ClashX Meta/1.4.0", "clash"},
		{"SFA/1.10.0 (sing-box 1.9.0)", "sing-box"},
	} {
		exporter, known := registry.Match(want.agent)
		if !known || exporter.ID() != want.id {
			t.Fatalf("%q resolved to %v (%v), want %s", want.agent, exporter, known, want.id)
		}
	}
	for _, agent := range []string{"", "curl/8.5.0"} {
		if exporter, known := registry.Match(agent); known {
			t.Fatalf("%q resolved to %v", agent, exporter)
		}
	}
}
