package subscription

import (
	"encoding/json"
	"proxycontrol/internal/panel/subscription/rules"
	"reflect"
	"testing"
)

func TestPresetsRecommendedDNS(t *testing.T) {
	p := newRulesPanel(t)
	viewer, _ := sessionFor(t, p.database, "Viewer")
	response := p.do(t, "GET", "/api/v1/subscription-rules/presets", "", viewer, "")
	var got struct {
		Items          []rules.Preset `json:"items"`
		RecommendedDNS rules.DNS      `json:"recommendedDns"`
	}
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != len(rules.Presets()) || !reflect.DeepEqual(got.RecommendedDNS, rules.RecommendedDNS()) {
		t.Fatalf("response: %s", response.Body.String())
	}
}
