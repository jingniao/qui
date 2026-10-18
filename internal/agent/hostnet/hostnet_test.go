package hostnet

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testInterfaces is the interface lookup of the golden tests: the two addresses
// are assigned to eth0 and eth1, everything else is missing.
func testInterfaces(address string) (string, error) {
	switch address {
	case "198.51.100.4":
		return "eth0", nil
	case "2001:db8::4":
		return "eth1", nil
	}
	return "", fmt.Errorf("address %s is not assigned to any local interface", address)
}

// TestRenderRuleFile pins the complete rule file of every shape the helper can
// be asked to write. The table is created before it is deleted so a first
// install works, every rule stays in that one table, and an unconfigured family
// never appears.
func TestRenderRuleFile(t *testing.T) {
	cases := []struct {
		name    string
		desired Desired
		want    string
	}{
		{
			name:    "ipv4 only",
			desired: Desired{ID: "a", IPv4: "198.51.100.4"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth0" ip saddr != { 198.51.100.4 } snat ip to 198.51.100.4
  }
}
`,
		},
		{
			name:    "ipv4 with the core outbound exemption",
			desired: Desired{ID: "a", IPv4: "198.51.100.4", ExemptIPv4: "198.51.100.9"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth0" ip saddr != { 198.51.100.4, 198.51.100.9 } snat ip to 198.51.100.4
  }
}
`,
		},
		{
			name:    "ipv6 only",
			desired: Desired{ID: "a", IPv6: "2001:db8::4"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth1" ip6 saddr != { 2001:db8::4 } snat ip6 to 2001:db8::4
  }
}
`,
		},
		{
			name:    "ipv6 with the core outbound exemption",
			desired: Desired{ID: "a", IPv6: "2001:db8::4", ExemptIPv6: "2001:db8::9"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth1" ip6 saddr != { 2001:db8::4, 2001:db8::9 } snat ip6 to 2001:db8::4
  }
}
`,
		},
		{
			name:    "both families with the ipv4 core outbound exemption",
			desired: Desired{ID: "a", IPv4: "198.51.100.4", IPv6: "2001:db8::4", ExemptIPv4: "198.51.100.9"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth0" ip saddr != { 198.51.100.4, 198.51.100.9 } snat ip to 198.51.100.4
    oifname "eth1" ip6 saddr != { 2001:db8::4 } snat ip6 to 2001:db8::4
  }
}
`,
		},
		{
			name:    "both families with the ipv6 core outbound exemption",
			desired: Desired{ID: "a", IPv4: "198.51.100.4", IPv6: "2001:db8::4", ExemptIPv6: "2001:db8::9"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth0" ip saddr != { 198.51.100.4 } snat ip to 198.51.100.4
    oifname "eth1" ip6 saddr != { 2001:db8::4, 2001:db8::9 } snat ip6 to 2001:db8::4
  }
}
`,
		},
		{
			name:    "exemption of a family that is not enforced",
			desired: Desired{ID: "a", IPv6: "2001:db8::4", ExemptIPv4: "198.51.100.9"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth1" ip6 saddr != { 2001:db8::4 } snat ip6 to 2001:db8::4
  }
}
`,
		},
		{
			name:    "mapped ipv4 addresses render canonically",
			desired: Desired{ID: "a", IPv4: "::ffff:198.51.100.4", ExemptIPv4: "::ffff:198.51.100.9"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth0" ip saddr != { 198.51.100.4, 198.51.100.9 } snat ip to 198.51.100.4
  }
}
`,
		},
		{
			name:    "exemption equal to the source is omitted",
			desired: Desired{ID: "a", IPv4: "198.51.100.4", ExemptIPv4: "198.51.100.4"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_source {
  chain postrouting {
    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins
    oifname "eth0" ip saddr != { 198.51.100.4 } snat ip to 198.51.100.4
  }
}
`,
		},
		{
			name:    "clear",
			desired: Desired{ID: "a"},
			want: `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			rules, err := Render(test.desired, testInterfaces)
			if err != nil {
				t.Fatal(err)
			}
			if string(rules) != test.want {
				t.Fatalf("rendered rules differ:\n--- got ---\n%s--- want ---\n%s", rules, test.want)
			}
		})
	}
}

// TestRenderValidatesEveryField keeps the helper from trusting the file it reads:
// the shared contract validator decides, and an address that is not assigned
// locally is refused instead of producing a rule that can never match.
func TestRenderValidatesEveryField(t *testing.T) {
	cases := []struct {
		name    string
		desired Desired
	}{
		{name: "missing id", desired: Desired{IPv4: "198.51.100.4"}},
		{name: "ipv6 in the ipv4 field", desired: Desired{ID: "a", IPv4: "2001:db8::4"}},
		{name: "ipv4 in the ipv6 field", desired: Desired{ID: "a", IPv6: "198.51.100.4"}},
		{name: "loopback source", desired: Desired{ID: "a", IPv4: "127.0.0.1"}},
		{name: "unspecified source", desired: Desired{ID: "a", IPv6: "::"}},
		{name: "unparsable source", desired: Desired{ID: "a", IPv4: "not-an-address"}},
		{name: "exemption of the other family", desired: Desired{ID: "a", IPv4: "198.51.100.4", ExemptIPv4: "2001:db8::9"}},
		{name: "exemption that is not unicast", desired: Desired{ID: "a", IPv4: "198.51.100.4", ExemptIPv4: "169.254.1.1"}},
		{name: "missing address", desired: Desired{ID: "a", IPv4: "203.0.113.7"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Render(test.desired, testInterfaces); err == nil {
				t.Fatalf("invalid desired state rendered: %+v", test.desired)
			}
		})
	}
}

// TestHostInterfaceLookupFindsTheLoopbackAddress checks the production lookup
// against an address every host has.
func TestHostInterfaceLookupFindsTheLoopbackAddress(t *testing.T) {
	name, err := HostInterfaceLookup("127.0.0.1")
	if err != nil {
		t.Fatalf("loopback address was not found: %v", err)
	}
	if name == "" {
		t.Fatal("loopback address resolved to an empty interface name")
	}
	if _, err := HostInterfaceLookup("203.0.113.7"); err == nil {
		t.Fatal("an address that is not assigned locally was accepted")
	}
}

// TestRunNftReportsAMissingBinary documents the fixed lookup: nft is only taken
// from the fixed locations, never from the environment's PATH.
func TestRunNftReportsAMissingBinary(t *testing.T) {
	paths := nftPaths
	nftPaths = []string{filepath.Join(t.TempDir(), "nft")}
	defer func() { nftPaths = paths }()
	t.Setenv("PATH", t.TempDir())
	err := RunNft(context.Background(), []byte("table inet x {}\n"))
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("missing nft was not reported: %v", err)
	}
}

// TestRunHelperIsANoOpWithoutADesiredFile is the boot case: the unit starts
// before the first publish and must succeed without calling nft.
func TestRunHelperIsANoOpWithoutADesiredFile(t *testing.T) {
	calls := 0
	restore := applyDesired
	applyDesired = func(context.Context, Desired) error { calls++; return nil }
	defer func() { applyDesired = restore }()
	if err := RunHelper(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("absent desired file failed the helper: %v", err)
	}
	if calls != 0 {
		t.Fatal("the helper applied rules without a desired file")
	}
}

// TestRunHelperPublishesTheOutcome pins the file protocol: the status carries the
// id of the desired file it answers, and a failure is reported as a status
// instead of only as an exit code.
func TestRunHelperPublishesTheOutcome(t *testing.T) {
	directory := t.TempDir()
	if err := WriteDesired(directory, Desired{ID: "publish-1", IPv4: "198.51.100.4", ExemptIPv4: "198.51.100.9"}); err != nil {
		t.Fatal(err)
	}
	var applied Desired
	restore := applyDesired
	applyDesired = func(_ context.Context, desired Desired) error { applied = desired; return nil }
	defer func() { applyDesired = restore }()
	if err := RunHelper(context.Background(), directory); err != nil {
		t.Fatal(err)
	}
	if applied.ID != "publish-1" || applied.IPv4 != "198.51.100.4" || applied.ExemptIPv4 != "198.51.100.9" {
		t.Fatalf("helper applied the wrong desired state: %+v", applied)
	}
	status, err := ReadStatus(directory)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != "publish-1" || !status.OK || status.Error != "" {
		t.Fatalf("successful helper reported %+v", status)
	}
	applyDesired = func(context.Context, Desired) error { return errors.New("nft -f - failed") }
	if err := RunHelper(context.Background(), directory); err == nil {
		t.Fatal("failed helper reported success")
	}
	status, err = ReadStatus(directory)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != "publish-1" || status.OK || status.Error != "nft -f - failed" {
		t.Fatalf("failed helper reported %+v", status)
	}
}

// TestRunHelperRejectsAnUnknownField keeps the desired file minimal and stable: a
// field this helper does not know means the file was written by a newer Agent,
// and guessing would silently drop a setting.
func TestRunHelperRejectsAnUnknownField(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, DesiredFileName)
	if err := os.WriteFile(path, []byte(`{"id":"a","ipv4":"198.51.100.4","new_field":"x"}`), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	restore := applyDesired
	applyDesired = func(context.Context, Desired) error { calls++; return nil }
	defer func() { applyDesired = restore }()
	err := RunHelper(context.Background(), directory)
	if err == nil || !strings.Contains(err.Error(), "new_field") {
		t.Fatalf("unknown field was not rejected: %v", err)
	}
	if calls != 0 {
		t.Fatal("an unknown field reached nft")
	}
}

// TestDesiredFilesRoundTripAndLeaveNoTemporaryFile covers the atomic publish the
// Agent and the helper share.
func TestDesiredFilesRoundTripAndLeaveNoTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	desired := Desired{ID: "round-trip", IPv6: "2001:db8::4", ExemptIPv6: "2001:db8::9"}
	if err := WriteDesired(directory, desired); err != nil {
		t.Fatal(err)
	}
	read, err := ReadDesired(directory)
	if err != nil {
		t.Fatal(err)
	}
	if read != desired {
		t.Fatalf("desired file lost a field: %+v", read)
	}
	if err := WriteStatus(directory, Status{ID: "round-trip", OK: true}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 2 || names[0] != DesiredFileName || names[1] != StatusFileName {
		t.Fatalf("helper directory holds unexpected files: %v", names)
	}
	if _, err := ReadDesired(filepath.Join(directory, "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing desired file did not report fs.ErrNotExist: %v", err)
	}
}
