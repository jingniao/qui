package desiredstate

import (
	"encoding/json"
	"strings"
	"testing"
)

func intPointer(value int) *int          { return &value }
func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }

// TestForwardOptionBoundaries pins every bound of the G2 forward settings. The
// Agent, the panel and the archive validation all read this one validation, so a
// bound that drifts here drifts everywhere.
func TestForwardOptionBoundaries(t *testing.T) {
	legacy := func() *ForwardSettings {
		return &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp"}
	}
	pool := func(targets ...ForwardTarget) *ForwardSettings {
		return &ForwardSettings{Network: "tcp,udp", Targets: targets}
	}
	for _, tc := range []struct {
		name     string
		forward  *ForwardSettings
		want     string
		accepted bool
	}{
		{"legacy single target", legacy(), "", true},
		{"pool of two", pool(ForwardTarget{"203.0.113.10", 443}, ForwardTarget{"target.example.net", 8443}), "", true},
		{"empty pool", &ForwardSettings{Network: "tcp", Targets: []ForwardTarget{}}, "target host", false},
		{"pool with legacy port", &ForwardSettings{Network: "tcp", Port: 443, Targets: []ForwardTarget{{Host: "203.0.113.10", Port: 443}}}, "cannot be combined", false},
		{"pool with legacy host", &ForwardSettings{Network: "tcp", Host: "203.0.113.10", Targets: []ForwardTarget{{Host: "203.0.113.10", Port: 443}}}, "cannot be combined", false},
		{"duplicate target", pool(ForwardTarget{"TARGET.example.net", 443}, ForwardTarget{"target.example.net", 443}), "unique", false},
		{"canonical duplicate IP", pool(ForwardTarget{"2001:db8::1", 443}, ForwardTarget{"2001:0db8:0000::1", 443}), "unique", false},
		{"bad target host", pool(ForwardTarget{"203.0.113.10", 0}), "target host", false},
		{"strategy round", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", Strategy: stringPointer("round")}, "", true},
		{"strategy fifo", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", Strategy: stringPointer("fifo")}, "", true},
		{"strategy weighted", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", Strategy: stringPointer("weighted")}, "round or fifo", false},
		{"max fails low", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxFails: intPointer(0)}, "max_fails", false},
		{"max fails high", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxFails: intPointer(101)}, "max_fails", false},
		{"fail timeout canonical", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("30s")}, "", true},
		{"fail timeout bare", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("30")}, "fail_timeout", false},
		{"fail timeout fraction", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("1.5s")}, "fail_timeout", false},
		{"fail timeout negative", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("-5s")}, "fail_timeout", false},
		{"fail timeout padded", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("05s")}, "fail_timeout", false},
		{"fail timeout too long", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("3601s")}, "fail_timeout", false},
		{"bandwidth zero", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", BandwidthInBps: int64Pointer(0)}, "", true},
		{"bandwidth floor", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", BandwidthInBps: int64Pointer(1024)}, "", true},
		{"bandwidth too small", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", BandwidthOutBps: int64Pointer(1023)}, "bandwidth", false},
		{"bandwidth too large", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", BandwidthOutBps: int64Pointer(1_000_000_000_001)}, "bandwidth", false},
		{"connections zero", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxConnections: intPointer(0)}, "", true},
		{"connections cap", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxConnections: intPointer(MaxForwardConnections)}, "", true},
		{"connections too many", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxConnections: intPointer(MaxForwardConnections + 1)}, "max_connections", false},
		{"connections on udp", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "udp", MaxConnections: intPointer(10)}, "requires a TCP service", false},
		{"connections on dual", &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp,udp", MaxConnections: intPointer(10)}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.forward.Validate()
			if tc.accepted {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

// TestForwardListenPorts pins the shared expansion: a forward without an end port
// is exactly its own port, a range is inclusive and bounded.
func TestForwardListenPorts(t *testing.T) {
	end := 20002
	state := ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", PortEnd: &end}}
	if ports := ListenPorts(20000, state); len(ports) != 3 || ports[0] != 20000 || ports[2] != 20002 {
		t.Fatalf("range expansion: %v", ports)
	}
	if ports := ListenPorts(20000, ProtocolSettings{}); len(ports) != 1 || ports[0] != 20000 {
		t.Fatalf("single port: %v", ports)
	}
	if ports := ListenPorts(20000, ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp"}}); len(ports) != 1 {
		t.Fatalf("forward without a range: %v", ports)
	}
	// The cap is enforced by validation and by the helper itself, so a stored row
	// can never expand into an unbounded loop.
	tooWide := 20000 + MaxForwardListenPorts + 100
	wide := ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", PortEnd: &tooWide}}
	if ports := ListenPorts(20000, wide); len(ports) != MaxForwardListenPorts {
		t.Fatalf("unbounded expansion: %d", len(ports))
	}
	if err := ValidateListenPorts(20000, wide); err == nil || !strings.Contains(err.Error(), "at most 256") {
		t.Fatalf("wide range accepted: %v", err)
	}
	for _, bad := range []int{0, 65536} {
		if err := ValidateListenPorts(bad, ProtocolSettings{}); err == nil {
			t.Fatalf("invalid base port %d accepted", bad)
		}
	}
	below := 19999
	if err := ValidateListenPorts(20000, ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp", PortEnd: &below}}); err == nil {
		t.Fatal("an end port below the base port was accepted")
	}
}

// TestForwardOptionsArePresenceBased proves the GOST-only fields are detected even
// when they carry a zero or a default: a core that cannot render them must refuse
// the inbound rather than silently drop the Owner's setting.
func TestForwardOptionsArePresenceBased(t *testing.T) {
	for _, forward := range []*ForwardSettings{
		{Host: "203.0.113.10", Port: 443, Network: "tcp", Targets: []ForwardTarget{{Host: "203.0.113.10", Port: 443}}},
		{Host: "203.0.113.10", Port: 443, Network: "tcp", Strategy: stringPointer("round")},
		{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxFails: intPointer(0)},
		{Host: "203.0.113.10", Port: 443, Network: "tcp", FailTimeout: stringPointer("5s")},
		{Host: "203.0.113.10", Port: 443, Network: "tcp", PortEnd: intPointer(0)},
		{Host: "203.0.113.10", Port: 443, Network: "tcp", BandwidthInBps: int64Pointer(0)},
		{Host: "203.0.113.10", Port: 443, Network: "tcp", MaxConnections: intPointer(0)},
	} {
		if !forward.HasGostOptions() {
			t.Fatalf("an explicitly written option was read as omitted: %+v", forward)
		}
		if err := ValidateForwardCore(CoreSingBox, ProtocolSettings{Forward: forward}); err == nil {
			t.Fatalf("a non-gost core accepted %+v", forward)
		}
		if err := ValidateForwardCore(CoreGost, ProtocolSettings{Forward: forward}); err != nil {
			t.Fatalf("gost refused its own option: %v", err)
		}
	}
	if (&ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp"}).HasGostOptions() {
		t.Fatal("a legacy forward reported GOST-only options")
	}
}

// TestForwardSelectorAndPoolDefaults keeps the rendered defaults in one place.
func TestForwardSelectorAndPoolDefaults(t *testing.T) {
	strategy, maxFails, failTimeout := (&ForwardSettings{Host: "203.0.113.10", Port: 443, Network: "tcp"}).Selector()
	if strategy != "round" || maxFails != 1 || failTimeout != "5s" {
		t.Fatalf("defaults: %s %d %s", strategy, maxFails, failTimeout)
	}
	configured := &ForwardSettings{Network: "tcp", Targets: []ForwardTarget{{Host: "203.0.113.10", Port: 443}}, Strategy: stringPointer("fifo"), MaxFails: intPointer(3), FailTimeout: stringPointer("12s")}
	if strategy, maxFails, failTimeout = configured.Selector(); strategy != "fifo" || maxFails != 3 || failTimeout != "12s" {
		t.Fatalf("configured selector: %s %d %s", strategy, maxFails, failTimeout)
	}
	if pool := (&ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp"}).TargetPool(); len(pool) != 1 || pool[0] != (ForwardTarget{Host: "203.0.113.10", Port: 8443}) {
		t.Fatalf("legacy pool: %+v", pool)
	}
}

// TestStateRefusesGostForwardOptionsOnAnotherCore covers the shared state
// validation: an inbound that names its core cannot carry the GOST-only options
// on another one, and a range reserves every number it listens on.
func TestStateRefusesGostForwardOptionsOnAnotherCore(t *testing.T) {
	state := State{SchemaVersion: 1, Revision: 1, NodeID: "n", Logging: Logging{DomainOnly: true},
		Inbounds: []Inbound{{
			ID: "forward", Core: CoreXray, Protocol: "forward", Listen: "127.0.0.1", Port: 20000, Enabled: true, DefaultOutbound: "direct",
			Settings: ProtocolSettings{Forward: &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp", MaxConnections: intPointer(10)}},
		}},
		Outbounds: []Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	if err := state.Validate(); err == nil || !strings.Contains(err.Error(), "require the gost core") {
		t.Fatalf("xray forward accepted GOST options: %v", err)
	}
	state.Inbounds[0].Core = CoreGost
	state.Inbounds[0].Settings.Forward = &ForwardSettings{Host: "203.0.113.10", Port: 8443, Network: "tcp", PortEnd: intPointer(20002)}
	state.Inbounds = append(state.Inbounds, Inbound{ID: "inside", Core: CoreXray, Protocol: "socks5", Listen: "127.0.0.1", Port: 20001, Enabled: true, DefaultOutbound: "direct"})
	if err := state.Validate(); err == nil || !strings.Contains(err.Error(), "port conflict") {
		t.Fatalf("a port inside the range was accepted: %v", err)
	}
	state.Inbounds[1].Port = 20100
	if err := state.Validate(); err != nil {
		t.Fatalf("a range next to an unrelated port was refused: %v", err)
	}
}

// TestForwardSettingsDecodeKeepsTheEmptyPoolInvalid pins the one shape a Go slice
// cannot express: an explicitly written `targets: []` must be refused, not read as
// "no pool". A legacy forward without the key still decodes and re-encodes to the
// same bytes, and unknown fields stay rejected.
func TestForwardSettingsDecodeKeepsTheEmptyPoolInvalid(t *testing.T) {
	var empty ForwardSettings
	if err := json.Unmarshal([]byte(`{"host":"203.0.113.10","port":443,"network":"tcp","targets":[]}`), &empty); err != nil {
		t.Fatal(err)
	}
	if err := empty.Validate(); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Fatalf("an explicit empty pool was accepted: %v", err)
	}
	var legacy ForwardSettings
	if err := json.Unmarshal([]byte(`{"host":"203.0.113.10","port":443,"network":"tcp"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy forward refused: %v", err)
	}
	if data, err := json.Marshal(legacy); err != nil || string(data) != `{"host":"203.0.113.10","port":443,"network":"tcp"}` {
		t.Fatalf("legacy bytes changed: %s %v", data, err)
	}
	var unknown ForwardSettings
	if err := json.Unmarshal([]byte(`{"host":"203.0.113.10","port":443,"network":"tcp","future":1}`), &unknown); err == nil {
		t.Fatal("an unknown forward field was accepted")
	}
}
