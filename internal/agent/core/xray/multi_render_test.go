package xray

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPerCoreRenderMatchesGoldenAndGuardsPeerPorts(t *testing.T) {
	for _, scenario := range goldenScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			s := scenario.state.ForCore("xray", "xray")
			r, err := renderWithOptions(context.Background(), s, goldenAt, goldenOptions)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", scenario.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(r.Data)) != strings.TrimSpace(string(want)) {
				t.Fatal("per-core closure changed the golden native config")
			}
		})
	}
	s := goldenScenarios()[0].state
	options := goldenOptions
	options.PrivatePorts = []int{15401, 15402, 15403}
	r, err := renderWithOptions(context.Background(), s, goldenAt, options)
	if err != nil {
		t.Fatal(err)
	}
	var doc configDocument
	if err := json.Unmarshal(r.Data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Routing.Rules) == 0 || doc.Routing.Rules[0].Port != "15401,15402,15403" {
		t.Fatal("peer private listeners unguarded")
	}
}
