package node

import (
	"context"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
	"time"
)

func TestSettingsCommitTogetherAndPreserveOmittedFields(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "Original", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	allow, maintenance, ip := false, true, "192.0.2.10"
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Updated", Enabled: true, AllowSubscription: &allow, Maintenance: &maintenance, PublicIP: &ip}); err != nil {
		t.Fatal(err)
	}
	before, err := r.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TRIGGER reject_node_settings BEFORE UPDATE ON nodes WHEN NEW.name='Rejected' BEGIN SELECT RAISE(FAIL,'settings write fault'); END`); err != nil {
		t.Fatal(err)
	}
	allow, maintenance, ip = true, false, "192.0.2.20"
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Rejected", Enabled: false, AllowSubscription: &allow, Maintenance: &maintenance, PublicIP: &ip}); err == nil {
		t.Fatal("settings fault ignored")
	}
	after, err := r.Get(ctx, n.ID)
	if err != nil || after.Name != before.Name || after.Enabled != before.Enabled || after.AllowSubscription != before.AllowSubscription || after.Maintenance != before.Maintenance || after.PublicIP != before.PublicIP || after.Status != before.Status {
		t.Fatal("partial settings survived", err)
	}
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Disabled", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	disabled, err := r.Get(ctx, n.ID)
	if err != nil || disabled.Enabled || disabled.Status != "disabled" || disabled.AllowSubscription || !disabled.Maintenance || disabled.PublicIP != "192.0.2.10" {
		t.Fatal(disabled, err)
	}
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Enabled", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	enabled, err := r.Get(ctx, n.ID)
	if err != nil || !enabled.Enabled || enabled.Status != "offline" {
		t.Fatal(enabled, err)
	}
	invalid := "invalid-address"
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Bad", Enabled: false, PublicIP: &invalid}); !errors.Is(err, ErrSettings) {
		t.Fatal(err)
	}
	if err := r.UpdateSettings(ctx, "missing", SettingsInput{Name: "Missing", Enabled: true}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSettingsStoreOutboundNetworkAndRejectInvalidValues(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "Network", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sourceIP, iface, ipv4, ipv6 := " 192.0.2.10 ", " eth0 ", "198.51.100.4", "2001:db8::4"
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Network", Enabled: true, OutboundSourceIP: &sourceIP, OutboundInterface: &iface, HostSourceIPv4: &ipv4, HostSourceIPv6: &ipv6}); err != nil {
		t.Fatal(err)
	}
	stored, err := r.Get(ctx, n.ID)
	if err != nil || stored.OutboundSourceIP != "192.0.2.10" || stored.OutboundInterface != "eth0" || stored.HostSourceIPv4 != "198.51.100.4" || stored.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("outbound network not stored trimmed: %+v %v", stored, err)
	}
	// An omitted field keeps its stored value, like the other optional settings.
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Network", Enabled: true, OutboundInterface: &iface}); err != nil {
		t.Fatal(err)
	}
	if kept, err := r.Get(ctx, n.ID); err != nil || kept.OutboundSourceIP != "192.0.2.10" || kept.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("omitted settings were cleared: %+v %v", kept, err)
	}
	// An empty value clears exactly one field.
	empty := ""
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Network", Enabled: true, OutboundSourceIP: &empty}); err != nil {
		t.Fatal(err)
	}
	if cleared, err := r.Get(ctx, n.ID); err != nil || cleared.OutboundSourceIP != "" || cleared.OutboundInterface != "eth0" || cleared.HostSourceIPv4 != "198.51.100.4" || cleared.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("clearing changed unrelated settings: %+v %v", cleared, err)
	}
	// The panel rejects exactly what the Agent's contract rejects, and a
	// rejected update leaves every stored value untouched.
	pointer := func(value string) *string { return &value }
	rejected := []struct {
		name  string
		input SettingsInput
	}{
		{name: "unspecified source", input: SettingsInput{OutboundSourceIP: pointer("0.0.0.0")}},
		{name: "loopback source", input: SettingsInput{OutboundSourceIP: pointer("127.0.0.1")}},
		{name: "multicast source", input: SettingsInput{OutboundSourceIP: pointer("239.1.2.3")}},
		{name: "unparsable source", input: SettingsInput{OutboundSourceIP: pointer("not-an-address")}},
		{name: "zoned source", input: SettingsInput{OutboundSourceIP: pointer("fe80::1%eth0")}},
		{name: "interface with space", input: SettingsInput{OutboundInterface: pointer("eth0 1")}},
		{name: "interface with slash", input: SettingsInput{OutboundInterface: pointer("eth0/1")}},
		{name: "interface too long", input: SettingsInput{OutboundInterface: pointer("0123456789abcdef")}},
		{name: "ipv6 in the ipv4 field", input: SettingsInput{HostSourceIPv4: pointer("2001:db8::4")}},
		{name: "ipv4 in the ipv6 field", input: SettingsInput{HostSourceIPv6: pointer("198.51.100.4")}},
		{name: "mapped ipv4 in the ipv6 field", input: SettingsInput{HostSourceIPv6: pointer("::ffff:198.51.100.4")}},
		{name: "loopback host", input: SettingsInput{HostSourceIPv4: pointer("127.0.0.1")}},
	}
	for _, test := range rejected {
		test.input.Name, test.input.Enabled = "Bad", true
		if err := r.UpdateSettings(ctx, n.ID, test.input); !errors.Is(err, ErrSettings) {
			t.Fatalf("%s accepted: %v", test.name, err)
		}
	}
	after, err := r.Get(ctx, n.ID)
	if err != nil || after.Name != "Network" || after.OutboundSourceIP != "" || after.OutboundInterface != "eth0" || after.HostSourceIPv4 != "198.51.100.4" || after.HostSourceIPv6 != "2001:db8::4" {
		t.Fatalf("rejected settings changed the node: %+v %v", after, err)
	}
}
