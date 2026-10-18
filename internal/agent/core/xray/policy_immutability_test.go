package xray

import (
	"context"
	"encoding/json"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
	"time"
)

func TestPolicyRenderingDoesNotMutateSnapshot(t *testing.T) {
	state := testState(1080, 1)
	state.Policies = []desiredstate.Policy{{ID: "policy", Name: "Case normalization", Priority: 10, Enabled: true, Match: desiredstate.Match{Domains: []string{"API.EXAMPLE.COM."}, CIDRs: []string{"192.0.2.15/24"}}, Action: desiredstate.Action{Type: "Block"}}}
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := RenderAt(context.Background(), state, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(state)
	if err != nil || string(after) != string(before) {
		t.Fatal("render changed immutable input")
	}
	second, err := RenderAt(context.Background(), state, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if err != nil || string(first.Data) != string(second.Data) {
		t.Fatal("render not deterministic", err)
	}
}
