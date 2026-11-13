package portal_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"proxycontrol/internal/panel/user"
)

// /portal/me and the Owner preview both serialise Service.Me, so its JSON is
// the contract the frontend reads.
func TestProfileCarriesTrafficResetFields(t *testing.T) {
	p := newPanel(t)
	ctx := context.Background()
	anchor := time.Now().UTC().AddDate(0, 0, -10).Truncate(time.Second)
	for _, c := range []struct {
		cycle  string
		anchor string
		reset  bool
	}{
		{"monthly", anchor.Format(time.RFC3339Nano), true},
		{"30days", anchor.Format(time.RFC3339Nano), true},
		{"none", "", false},
	} {
		account, err := p.users.Save(ctx, "", user.Input{Username: "u_" + c.cycle, DisplayName: c.cycle, Status: "active", AllowedProtocols: []string{"socks5"}, TrafficResetCycle: c.cycle, TrafficResetAnchor: c.anchor})
		if err != nil {
			t.Fatal(err)
		}
		me, err := p.handler.Service.Me(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(me)
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["trafficResetCycle"] != c.cycle {
			t.Fatalf("%s: cycle %v", c.cycle, fields["trafficResetCycle"])
		}
		next, present := fields["nextTrafficResetAt"]
		if !present {
			t.Fatalf("%s: nextTrafficResetAt missing", c.cycle)
		}
		if !c.reset {
			if next != nil {
				t.Fatalf("none: next reset %v", next)
			}
			continue
		}
		got, err := time.Parse(time.RFC3339Nano, next.(string))
		if err != nil || !got.After(time.Now()) {
			t.Fatalf("%s: next %v %v", c.cycle, next, err)
		}
		saved, err := p.users.Get(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		if want := user.NextResetAt(saved, time.Now(), time.UTC); want != next {
			t.Fatalf("%s: portal %v, shared function %v", c.cycle, next, want)
		}
	}
}
