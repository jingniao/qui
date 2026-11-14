package hostnet

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenderExtraExempt(t *testing.T) {
	d := Desired{ID: "a", IPv4: "198.51.100.4", IPv6: "2001:db8::4", ExemptIPv4: "198.51.100.9", ExemptIPv6: "2001:db8::9",
		ExtraExemptIPv4: []string{"198.51.100.4", "::ffff:198.51.100.9", "198.51.100.10", "198.51.100.10"},
		ExtraExemptIPv6: []string{"2001:db8::4", "2001:db8::9", "2001:db8::10", "2001:db8:0::10"}}
	rules, err := Render(d, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ip saddr != { 198.51.100.4, 198.51.100.9, 198.51.100.10 }", "ip6 saddr != { 2001:db8::4, 2001:db8::9, 2001:db8::10 }"} {
		if !strings.Contains(string(rules), want) {
			t.Fatalf("missing %q in %s", want, rules)
		}
	}
}

func TestRenderExtraExemptIPv6(t *testing.T) {
	d := Desired{
		ID:              "ipv6-only",
		IPv6:            "2001:db8::4",
		ExemptIPv6:      "2001:db8::9",
		ExtraExemptIPv6: []string{"2001:db8::4", "2001:db8::9", "2001:db8::10", "2001:db8:0::10", "2001:db8::11"},
	}
	rules, err := Render(d, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	want := `oifname "eth1" ip6 saddr != { 2001:db8::4, 2001:db8::9, 2001:db8::10, 2001:db8::11 } snat ip6 to 2001:db8::4`
	if !strings.Contains(string(rules), want) {
		t.Fatalf("missing merged IPv6 rule %q in %s", want, rules)
	}
	if strings.Contains(string(rules), "ip saddr") || strings.Contains(string(rules), "snat ip to") {
		t.Fatalf("IPv6-only request rendered an IPv4 rule: %s", rules)
	}
}

func TestValidateExtraExempt(t *testing.T) {
	for _, family := range []string{"v4", "v6"} {
		valid, wrong := "198.51.100.9", "2001:db8::9"
		if family == "v6" {
			valid, wrong = wrong, valid
		}
		for _, value := range []string{valid, wrong, "", "127.0.0.1", "224.0.0.1", "::", "ff02::1", "fe80::1"} {
			t.Run(family+"/"+value, func(t *testing.T) {
				d := Desired{ID: "a"}
				if family == "v4" {
					d.ExtraExemptIPv4 = []string{value}
				} else {
					d.ExtraExemptIPv6 = []string{value}
				}
				if err := d.Validate(); (err == nil) != (value == valid) {
					t.Fatalf("validation of %q: %v", value, err)
				}
			})
		}
		for _, size := range []int{16, 17} {
			d := Desired{ID: "a"}
			addresses := make([]string, size)
			for i := range addresses {
				addresses[i] = valid
			}
			if family == "v4" {
				d.ExtraExemptIPv4 = addresses
			} else {
				d.ExtraExemptIPv6 = addresses
			}
			if err := d.Validate(); (err == nil) != (size == 16) {
				t.Fatalf("%s size %d: %v", family, size, err)
			}
		}
	}
}

func TestLegacyDesiredAndFeatureReceipt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DesiredFileName), []byte(`{"id":"legacy","ipv4":"","ipv6":"","exempt_ipv4":"","exempt_ipv6":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDesired(dir)
	if err != nil || d.ID != "legacy" {
		t.Fatalf("legacy request: %+v, %v", d, err)
	}
	previous := applyDesired
	defer func() { applyDesired = previous }()
	applyDesired = func(context.Context, Desired) error { return nil }
	if err := RunHelper(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatus(dir)
	if err != nil || !reflect.DeepEqual(status.Features, []string{FeatureExtraExempt}) {
		t.Fatalf("receipt: %+v, %v", status, err)
	}
}
