package policy

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/system"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestBaseTimezoneChangeKeepsSavedRuleAndPublishedSnapshotTimezone(t *testing.T) {
	service, nodeID, _, _, _ := fixture(t)
	ctx := context.Background()
	settings := service.Settings.(*system.Repository)
	input := Input{Name: "Timed block", Priority: 10, Enabled: true, Match: desiredstate.Match{Domains: []string{"example.test"}, Network: "tcp", TimeRange: "08:00-09:00"}, Action: desiredstate.Action{Type: "Block"}}
	first, err := service.Save(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Timezone != "Asia/Shanghai" {
		t.Fatal("initial inherited timezone missing", first.Timezone)
	}
	snapshot, err := service.ForNode(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(snapshot)
	if _, _, err := settings.SaveBase(ctx, system.BaseInput{PlatformName: "Timezone panel", Timezone: "America/New_York"}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(snapshot)
	if string(before) != string(after) {
		t.Fatal("base save changed an existing immutable snapshot")
	}
	current, err := service.ForNode(ctx, nodeID)
	if err != nil || len(current) != 1 || current[0].Timezone != "Asia/Shanghai" {
		t.Fatal("saved rule implicitly changed timezone", current, err)
	}
	input.Name = "New inherited rule"
	second, err := service.Save(ctx, "", input)
	if err != nil || second.Timezone != "America/New_York" {
		t.Fatal("new rules did not inherit new system timezone", second.Timezone, err)
	}
}
