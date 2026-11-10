package hostnet

import (
	"strings"
	"testing"
)

func TestRenderHopTable(t *testing.T) {
	desired := Desired{ID: "a", PortHops: []PortHop{
		{Start: 30000, End: 30010, Target: 8444},
		{Start: 20000, End: 20010, Target: 8443},
	}}
	rules, err := Render(desired, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	want := `table inet proxy_agent_source {} # makes the delete safe on first install
delete table inet proxy_agent_source
table inet proxy_agent_hop {} # makes the delete safe on first install
delete table inet proxy_agent_hop
table inet proxy_agent_hop {
  chain prerouting {
    type nat hook prerouting priority dstnat; policy accept;
    udp dport 20000-20010 redirect to :8443
    udp dport 30000-30010 redirect to :8444
  }
}
`
	if string(rules) != want {
		t.Fatalf("rendered rules differ:\n--- got ---\n%s--- want ---\n%s", rules, want)
	}
}

// Removing the last hop inbound must still delete the table, so a clear render
// carries the create-and-delete pair but no chain.
func TestRenderWithoutHopsDeletesTable(t *testing.T) {
	rules, err := Render(Desired{ID: "a"}, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	got := string(rules)
	if !strings.Contains(got, "delete table inet "+HopTable) {
		t.Fatalf("a clear render did not delete the hop table:\n%s", got)
	}
	if strings.Contains(got, "chain prerouting") {
		t.Fatalf("a clear render kept a hop chain:\n%s", got)
	}
}

// The source table and the hop table are independent: rendering one never
// removes or rewrites the other.
func TestRenderKeepsSourceAndHopTables(t *testing.T) {
	desired := Desired{ID: "a", IPv4: "198.51.100.4", PortHops: []PortHop{{Start: 20000, End: 20010, Target: 8443}}}
	rules, err := Render(desired, testInterfaces)
	if err != nil {
		t.Fatal(err)
	}
	got := string(rules)
	for _, part := range []string{
		"table inet " + Table + " {\n",
		"oifname \"eth0\" ip saddr != { 198.51.100.4 } snat ip to 198.51.100.4",
		"table inet " + HopTable + " {\n",
		"udp dport 20000-20010 redirect to :8443",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("render lost %q:\n%s", part, got)
		}
	}
}

func TestDesiredRejectsInvalidPortHops(t *testing.T) {
	many := make([]PortHop, 0, MaxPortHops+1)
	for i := 0; i <= MaxPortHops; i++ {
		many = append(many, PortHop{Start: 30000 + i*2, End: 30001 + i*2, Target: 8443})
	}
	cases := []struct {
		name  string
		hops  []PortHop
	}{
		{"end below start", []PortHop{{Start: 20010, End: 20000, Target: 8443}}},
		{"start below 1", []PortHop{{Start: 0, End: 20010, Target: 8443}}},
		{"end above 65535", []PortHop{{Start: 65530, End: 65536, Target: 8443}}},
		{"target out of range", []PortHop{{Start: 20000, End: 20010, Target: 0}}},
		{"overlapping ranges", []PortHop{{Start: 20000, End: 20010, Target: 8443}, {Start: 20005, End: 20015, Target: 8444}}},
		{"too many", many},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := (Desired{ID: "a", PortHops: test.hops}).Validate(); err == nil {
				t.Fatal("invalid hop set accepted")
			}
		})
	}
	if err := (Desired{ID: "a", PortHops: []PortHop{{Start: 20000, End: 20010, Target: 8443}}}).Validate(); err != nil {
		t.Fatal("valid hop set refused:", err)
	}
	if !(Desired{ID: "a"}).Clear() {
		t.Fatal("a fully empty desired is not clear")
	}
	if (Desired{ID: "a", PortHops: []PortHop{{Start: 20000, End: 20010, Target: 8443}}}).Clear() {
		t.Fatal("a desired with hops was reported as clear")
	}
}
