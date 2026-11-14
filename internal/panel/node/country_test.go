package node

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The flag before a server name comes from countryCode, which the repository
// derives from the current public address on every read (v1.5.5 item 19). It
// follows the address as it changes and is never stored.
func TestNodeCountryFollowsPublicIP(t *testing.T) {
	ctx := context.Background()
	r, id := machineFixture(t)
	if n := storedNode(t, r, id); n.CountryCode != "" {
		t.Fatalf("no address yet: %q", n.CountryCode)
	}
	recordPublicHeartbeat(t, r, ctx, id, 1, "5.45.96.43")
	if n := storedNode(t, r, id); n.CountryCode != "DE" {
		t.Fatalf("detected address: %q", n.CountryCode)
	}
	manual := "1.0.1.5"
	if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, PublicIP: &manual}); err != nil {
		t.Fatal(err)
	}
	n := storedNode(t, r, id)
	if n.CountryCode != "CN" {
		t.Fatalf("manual address: %q", n.CountryCode)
	}
	listed, _, err := r.List(ctx, "", 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].CountryCode != "CN" {
		t.Fatalf("list: %+v", listed)
	}
	body, err := json.Marshal(n)
	if err != nil || !strings.Contains(string(body), `"countryCode":"CN"`) {
		t.Fatalf("json: %s %v", body, err)
	}
	private := "10.0.0.8"
	if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, PublicIP: &private}); err != nil {
		t.Fatal(err)
	}
	if n := storedNode(t, r, id); n.CountryCode != "" {
		t.Fatalf("private address: %q", n.CountryCode)
	}
}
